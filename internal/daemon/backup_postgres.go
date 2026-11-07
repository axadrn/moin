package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/protocol"
)

// postgresEngine backs up a Postgres service with the tools of its own
// image, through docker exec: pg_dump -Fc streams a copy straight into
// the bucket, and a restore streams it from there into pg_restore. It
// works on every image, Alpine too. Nothing waits on the server's disk.
type postgresEngine struct {
	b        *backups
	app      string
	database protocol.Database
}

// target is the Postgres of the engine's database: the one POSTGRES_DB
// names, or another of the service.
func (e *postgresEngine) target(ctx context.Context) (pgTarget, error) {
	t, err := e.b.apps.postgres(ctx, e.app, e.database)
	if e.database.Name != "" {
		t.database = e.database.Name
	}
	return t, err
}

// databases are the databases of an app's live deploy, with every database
// of each Postgres service: postgres-db for the one POSTGRES_DB names,
// postgres-db/<name> for each other, but the templates and moin's own
// (moin_…).
func (b *backups) databases(ctx context.Context, app string) []protocol.Database {
	var all []protocol.Database
	for _, database := range b.apps.runningDatabases(app) {
		all = append(all, database)
		if !database.Postgres() {
			continue
		}
		t, err := b.apps.postgres(ctx, app, database)
		if err != nil {
			continue
		}
		out, err := admin(ctx, t, `SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY datname;`, nil)
		for _, name := range strings.Split(out, "\n") {
			if err == nil && name != "" && name != t.database && !strings.HasPrefix(name, "moin_") {
				all = append(all, protocol.Database{Path: database.Path + "/" + name, Kind: database.Kind, Service: database.Service, Name: name})
			}
		}
	}
	return all
}

func (e *postgresEngine) Copy(ctx context.Context, w io.Writer) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	return t.exec(ctx, nil, w, "pg_dump", "-Fc", "-h", pgSocket, "-U", t.user, "-d", t.database)
}

// admin runs SQL outside the app's database, in template1, which every
// Postgres has. vars are psql variables: :'name' a literal, :"name" a name.
func admin(ctx context.Context, t pgTarget, sql string, vars map[string]string) (string, error) {
	t.database = "template1"
	out, err := t.psql(ctx, sql, vars, false)
	return strings.TrimSpace(out), err
}

// load creates the database name and streams a copy into it.
func load(ctx context.Context, t pgTarget, name string, copy io.Reader) error {
	_, err := admin(ctx, t, `DROP DATABASE IF EXISTS :"db"; CREATE DATABASE :"db" TEMPLATE template0;`, map[string]string{"db": name})
	if err != nil {
		return err
	}
	if err := t.exec(ctx, copy, io.Discard, "pg_restore", "--exit-on-error", "-h", pgSocket, "-U", t.user, "-d", name); err != nil {
		_, _ = admin(context.WithoutCancel(ctx), t, `DROP DATABASE IF EXISTS :"db";`, map[string]string{"db": name})
		return fmt.Errorf("pg_restore: %w", err)
	}
	return nil
}

func dbExists(ctx context.Context, t pgTarget, name string) (bool, error) {
	out, err := admin(ctx, t, `SELECT count(*) FROM pg_database WHERE datname = :'db';`, map[string]string{"db": name})
	return out == "1", err
}

// rename renames a database once nobody is connected to it.
func rename(ctx context.Context, t pgTarget, from, to string) error {
	var err error
	for try := 0; try < 10; try++ {
		_, err = admin(ctx, t, `SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE datname = :'from' AND pid <> pg_backend_pid();
ALTER DATABASE :"from" RENAME TO :"to";`, map[string]string{"from": from, "to": to})
		if err == nil || !strings.Contains(err.Error(), "being accessed") {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

// The databases moin makes all start with moin_, and are named after a
// short hash of the database they are for, so they always fit Postgres's
// 63 characters: moin_restore_<hash> (being built), moin_check_<hash>
// (the daily check), moin_before_<id>_<hash> (an undo).
func moinName(what, database string) string {
	sum := sha256.Sum256([]byte(database))
	return "moin_" + what + "_" + hex.EncodeToString(sum[:4])
}

func undoName(database, id string) string {
	return strings.Replace(moinName("before", database), "_before_", "_before_"+strings.ToLower(id)+"_", 1)
}

// pgRoom says when the volume of the Postgres has less free than the
// database takes, plus 512 MB: a restore or a check builds one more.
func pgRoom(ctx context.Context, t pgTarget) error {
	out, err := admin(ctx, t, `SELECT pg_database_size(:'db') || ' ' || current_setting('data_directory');`, map[string]string{"db": t.database})
	need, dir, _ := strings.Cut(out, " ")
	var df bytes.Buffer
	if err == nil {
		err = t.exec(ctx, nil, &df, "df", "-Pk", dir)
	}
	var size, free int64
	fields := strings.Fields(df.String())
	if _, scanErr := fmt.Sscan(need, &size); err == nil && (scanErr != nil || len(fields) < 10) {
		err = errors.New("df said nothing")
	}
	if err != nil {
		return fmt.Errorf("see how much room the Postgres has: %w", err)
	}
	fmt.Sscan(fields[len(fields)-3], &free)
	if free*1024 < size+512<<20 {
		return fmt.Errorf("the volume of the Postgres has %s free and this needs %s: free some space", human.Size(float64(free*1024)), human.Size(float64(size+512<<20)))
	}
	return nil
}

// Restore loads a copy into moin_restore_<hash> while the app runs, with
// the owner of the live database. Then, with the app's other services
// stopped, it renames the live database to moin_before_<id>_<hash>, the
// undo, and the built one in. Settings and grants of the database itself
// (ALTER DATABASE … SET, GRANT … ON DATABASE) are not in a copy.
func (e *postgresEngine) Restore(ctx context.Context, from source, id string, step func(string) error) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	live, built, aside := t.database, moinName("restore", t.database), undoName(t.database, id)
	if from.undo != "" {
		built = undoName(live, from.undo)
		if found, err := dbExists(ctx, t, built); !found || err != nil {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
	} else if err := errors.Join(pgRoom(ctx, t), load(ctx, t, built, from.copy)); err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	} else if _, err := admin(ctx, t, `SELECT pg_get_userbyid(datdba) AS owner FROM pg_database WHERE datname = :'live' \gset
ALTER DATABASE :"built" OWNER TO :"owner";`, map[string]string{"live": live, "built": built}); err != nil {
		return fmt.Errorf("give the restored database its owner: %w; nothing changed", err)
	}
	start := func() error { return nil }
	if others := e.others(); len(others) > 0 {
		if start, err = e.b.apps.stopServices(ctx, e.app, others...); err != nil {
			return err
		}
	}
	ctx = context.WithoutCancel(ctx)
	if err := step("switching"); errors.Is(err, errCrash) {
		return err
	} else if err != nil {
		return errors.Join(fmt.Errorf("write the restore down: %w; nothing changed", err), start())
	}
	if err := rename(ctx, t, live, aside); err != nil {
		return errors.Join(fmt.Errorf("switch: %w; nothing changed", err), start())
	}
	if err := step("aside"); err != nil {
		return err
	}
	if err := rename(ctx, t, built, live); err != nil {
		return errors.Join(fmt.Errorf("switch: %w", err), rename(ctx, t, aside, live), start())
	}
	if err := step("switched"); err != nil {
		return err
	}
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

// others are the app's services but the database, which keeps running.
func (e *postgresEngine) others() []string {
	var config struct {
		Services map[string]json.RawMessage `json:"services"`
	}
	body, _ := os.ReadFile(e.b.apps.composeFile(e.app))
	_ = json.Unmarshal(body, &config)
	others := []string{}
	for name := range config.Services {
		if name != e.database.Service {
			others = append(others, name)
		}
	}
	return others
}

// Recover puts the database back by the crash rule (recoverFrom), once
// the Postgres answers, up to two minutes: a database in the live place
// that is not the restored one goes aside as an undo.
func (e *postgresEngine) Recover(ctx context.Context, mark restoreMark) (bool, error) {
	t, err := e.target(ctx)
	if err == nil {
		_, err = admin(ctx, t, `SELECT 1;`, nil)
	}
	for try := 0; err != nil && try < 60; try++ {
		time.Sleep(2 * time.Second)
		if t, err = e.target(ctx); err == nil {
			_, err = admin(ctx, t, `SELECT 1;`, nil)
		}
	}
	if err != nil {
		return false, err
	}
	live, aside, built := t.database, undoName(t.database, mark.Undo), moinName("restore", t.database)
	if mark.From != "" {
		built = undoName(live, mark.From)
	}
	asideThere, err1 := dbExists(ctx, t, aside)
	builtThere, err2 := dbExists(ctx, t, built)
	liveThere, err3 := dbExists(ctx, t, live)
	if err := errors.Join(err1, err2, err3); err != nil {
		return false, err
	}
	switched, err := recoverFrom(mark.Step, asideThere, builtThere || !liveThere, func() error {
		if liveThere {
			// The app stops first, so it writes into no undo.
			if others := e.others(); len(others) > 0 {
				if _, err := e.b.apps.stopServices(ctx, e.app, others...); err != nil {
					return err
				}
			}
			if err := rename(ctx, t, live, undoName(live, strayID(mark.Undo))); err != nil {
				return err
			}
		}
		return rename(ctx, t, aside, live)
	})
	if err != nil {
		return false, err
	}
	_, err = admin(ctx, t, `DROP DATABASE IF EXISTS :"db";`, map[string]string{"db": moinName("restore", live)})
	return switched, err
}

// Check loads a copy into the throwaway database moin_check_<hash> and
// reads every row of every table, then drops it.
func (e *postgresEngine) Check(ctx context.Context, copy io.Reader) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	check := t
	check.database = moinName("check", t.database)
	if err := errors.Join(pgRoom(ctx, t), load(ctx, t, check.database, copy)); err != nil {
		return err
	}
	defer admin(context.WithoutCancel(ctx), t, `DROP DATABASE IF EXISTS :"db";`, map[string]string{"db": check.database})
	_, err = check.psql(ctx, `DO $$ DECLARE t record; BEGIN
FOR t IN SELECT schemaname, tablename FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema') LOOP
	EXECUTE format('SELECT md5(string_agg(x::text, '''')) FROM %I.%I x', t.schemaname, t.tablename);
END LOOP; END $$;`, nil, false)
	return err
}

func (e *postgresEngine) Undos(ctx context.Context) ([]protocol.Copy, error) {
	t, err := e.target(ctx)
	if err != nil {
		return nil, err
	}
	hash := strings.TrimPrefix(moinName("before", t.database), "moin_before_")
	out, err := admin(ctx, t, `SELECT datname || ' ' || pg_database_size(datname) FROM pg_database WHERE datname LIKE 'moin\_before\_%\_' || :'hash' ORDER BY datname DESC;`, map[string]string{"hash": hash})
	undos := []protocol.Copy{}
	for _, line := range strings.Split(out, "\n") {
		name, size, _ := strings.Cut(line, " ")
		id := strings.ToUpper(strings.TrimSuffix(strings.TrimPrefix(name, "moin_before_"), "_"+hash))
		if at, err := time.Parse(protocol.CopyIDLayout, id); err == nil {
			var bytes int64
			fmt.Sscan(size, &bytes)
			undos = append(undos, protocol.Copy{ID: id, At: at, Size: bytes})
		}
	}
	return undos, err
}

func (e *postgresEngine) Drop(ctx context.Context, id string) error {
	t, err := e.target(ctx)
	if err == nil {
		_, err = admin(ctx, t, `DROP DATABASE IF EXISTS :"db";`, map[string]string{"db": undoName(t.database, id)})
	}
	return err
}
