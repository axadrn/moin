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
	for _, database := range b.apps.liveDatabases(app) {
		all = append(all, database)
		if !database.Postgres() {
			continue
		}
		t, err := b.apps.postgres(ctx, app, database)
		if err != nil {
			continue
		}
		out, err := admin(ctx, t, `SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY datname;`, nil)
		if err != nil {
			continue
		}
		for _, name := range strings.Split(out, "\n") {
			if name != "" && name != t.database && !strings.HasPrefix(name, "moin_") {
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

// swap renames live to aside and built to live in one transaction, once
// nobody is connected to either.
func swap(ctx context.Context, t pgTarget, live, aside, built string) error {
	var err error
	for try := 0; try < 10; try++ {
		_, err = admin(ctx, t, `SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE datname IN (:'live', :'built') AND pid <> pg_backend_pid();
BEGIN;
ALTER DATABASE :"live" RENAME TO :"aside";
ALTER DATABASE :"built" RENAME TO :"live";
COMMIT;`, map[string]string{"live": live, "aside": aside, "built": built})
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

// Size is how many bytes the database takes.
func (e *postgresEngine) Size(ctx context.Context) (int64, error) {
	t, err := e.target(ctx)
	if err != nil {
		return 0, err
	}
	out, err := admin(ctx, t, `SELECT pg_database_size(:'db');`, map[string]string{"db": t.database})
	if err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	var size int64
	if _, err := fmt.Sscan(out, &size); err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	return size, nil
}

// pgRoom says when the volume of the Postgres has less free than need
// plus spareRoom: a restore or a check builds one more database.
func pgRoom(ctx context.Context, t pgTarget, need int64) error {
	dir, err := admin(ctx, t, `SELECT current_setting('data_directory');`, nil)
	if err != nil {
		return fmt.Errorf("see how much room the Postgres has: %w", err)
	}
	var df bytes.Buffer
	if err := t.exec(ctx, nil, &df, "df", "-Pk", dir); err != nil {
		return fmt.Errorf("see how much room the Postgres has: %w", err)
	}
	fields := strings.Fields(df.String())
	if len(fields) < 10 {
		return errors.New("see how much room the Postgres has: df said nothing")
	}
	var freeKB int64
	fmt.Sscan(fields[len(fields)-3], &freeKB)
	free := freeKB * 1024
	if free < need+spareRoom {
		return fmt.Errorf("the volume of the Postgres has %s free and this needs %s: free some space", human.Size(float64(free)), human.Size(float64(need+spareRoom)))
	}
	return nil
}

// Restore loads a copy into moin_restore_<hash> while the app runs, with
// the owner of the live database. Then, with the app's other services
// stopped, one transaction renames the live database to
// moin_before_<id>_<hash>, the undo, and the built one in: never none.
// Settings and grants of the database itself (ALTER DATABASE … SET,
// GRANT … ON DATABASE) are not in a copy.
func (e *postgresEngine) Restore(ctx context.Context, from source, keepAs string) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	live := t.database
	aside := undoName(live, keepAs)
	var incoming string
	if from.undo != "" {
		incoming = undoName(live, from.undo)
		found, err := dbExists(ctx, t, incoming)
		if err != nil {
			return fmt.Errorf("look for the state before the restore: %w; nothing changed", err)
		}
		if !found {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
	} else {
		incoming = moinName("restore", live)
		// After the switch this name is gone and the drop does nothing.
		defer e.drop(ctx, t, incoming)
		if err := e.build(ctx, t, from, incoming); err != nil {
			return fmt.Errorf("%w; nothing changed", err)
		}
	}
	// An app that is only its database has no other service to stop.
	start := func() error { return nil }
	if others := e.others(); len(others) > 0 {
		start, err = e.b.apps.stopServices(ctx, e.app, others...)
		if err != nil {
			return err
		}
	}
	ctx = context.WithoutCancel(ctx)
	if err := e.switchIn(ctx, t, live, aside, incoming); err != nil {
		return errors.Join(err, start())
	}
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

// build loads a copy into the database name, with the owner of the live
// one, once the volume has room.
func (e *postgresEngine) build(ctx context.Context, t pgTarget, from source, name string) error {
	if err := pgRoom(ctx, t, from.size); err != nil {
		return err
	}
	if err := load(ctx, t, name, from.copy); err != nil {
		return err
	}
	_, err := admin(ctx, t, `SELECT pg_get_userbyid(datdba) AS owner FROM pg_database WHERE datname = :'live' \gset
ALTER DATABASE :"built" OWNER TO :"owner";`, map[string]string{"live": t.database, "built": name})
	if err != nil {
		return fmt.Errorf("give the restored database its owner: %w", err)
	}
	return nil
}

// drop removes a database moin made, even when the restore was cancelled.
func (e *postgresEngine) drop(ctx context.Context, t pgTarget, name string) {
	_, _ = admin(context.WithoutCancel(ctx), t, `DROP DATABASE IF EXISTS :"db";`, map[string]string{"db": name})
}

// switchIn makes incoming the live database. When the answer to the switch
// gets lost it may have happened anyway: the undo says.
func (e *postgresEngine) switchIn(ctx context.Context, t pgTarget, live, aside, incoming string) error {
	err := swap(ctx, t, live, aside, incoming)
	if err == nil {
		return nil
	}
	switched, lookErr := dbExists(ctx, t, aside)
	if lookErr != nil {
		return fmt.Errorf("moin cannot tell whether the restore switched (%w): look at the app's data", err)
	}
	if switched {
		return nil
	}
	return fmt.Errorf("switch: %w; nothing changed", err)
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

// Recover tells from the undo, once the Postgres answers, up to two
// minutes: there means the switch happened, as both renames are one
// transaction.
func (e *postgresEngine) Recover(ctx context.Context, mark restoreMark) (bool, error) {
	t, err := e.waitForPostgres(ctx)
	if err != nil {
		return false, err
	}
	switched, err := dbExists(ctx, t, undoName(t.database, mark.Undo))
	if err != nil {
		return false, err
	}
	if !switched && mark.From == "" {
		_, err = admin(ctx, t, `DROP DATABASE IF EXISTS :"db";`, map[string]string{"db": moinName("restore", t.database)})
	}
	return switched, err
}

// waitForPostgres waits up to two minutes for the Postgres to answer.
func (e *postgresEngine) waitForPostgres(ctx context.Context) (pgTarget, error) {
	var err error
	for try := 0; try <= 60; try++ {
		if try > 0 {
			time.Sleep(2 * time.Second)
		}
		var t pgTarget
		t, err = e.target(ctx)
		if err != nil {
			continue
		}
		if _, err = admin(ctx, t, `SELECT 1;`, nil); err == nil {
			return t, nil
		}
	}
	return pgTarget{}, err
}

// Check loads a copy into the throwaway database moin_check_<hash> and
// reads every row of every table, then drops it.
func (e *postgresEngine) Check(ctx context.Context, from source) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	check := t
	check.database = moinName("check", t.database)
	if err := pgRoom(ctx, t, from.size); err != nil {
		return err
	}
	defer admin(context.WithoutCancel(ctx), t, `DROP DATABASE IF EXISTS :"db";`, map[string]string{"db": check.database})
	if err := load(ctx, t, check.database, from.copy); err != nil {
		return err
	}
	_, err = check.psql(ctx, `DO $$ DECLARE t record; BEGIN
FOR t IN SELECT schemaname, tablename FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema') LOOP
	EXECUTE format('SELECT sum(length(x::text)) FROM %I.%I x', t.schemaname, t.tablename);
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
		if at, ok := protocol.CopyTime(id); ok {
			var sizeBytes int64
			fmt.Sscan(size, &sizeBytes)
			undos = append(undos, protocol.Copy{ID: id, At: at, Size: sizeBytes})
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
