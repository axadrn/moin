package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
// postgres-db/<name> for each other, but the templates and what moin makes
// for a restore or a check.
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
			if err == nil && name != "" && name != t.database && !strings.HasSuffix(name, "_restore") && !strings.HasSuffix(name, "_check") && !strings.Contains(name, "_before_") {
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

func dbExists(ctx context.Context, t pgTarget, name string) bool {
	out, err := admin(ctx, t, `SELECT count(*) FROM pg_database WHERE datname = :'db';`, map[string]string{"db": name})
	return err == nil && out == "1"
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

func undoName(database, id string) string { return database + "_before_" + strings.ToLower(id) }

// Restore loads a copy into <db>_restore while the app runs. Then, with
// the app's other services stopped, it renames <db> to <db>_before_<id>,
// the undo, and <db>_restore to <db>.
func (e *postgresEngine) Restore(ctx context.Context, from source, id string) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	live, built, aside := t.database, t.database+"_restore", undoName(t.database, id)
	if len(aside) > 63 {
		return errors.New("the database's name is too long for a restore: at most 39 characters")
	}
	if from.undo != "" {
		built = undoName(live, from.undo)
		if !dbExists(ctx, t, built) {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
	} else if err := load(ctx, t, built, from.copy); err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	}
	start := func() error { return nil }
	if others := e.others(); len(others) > 0 {
		if start, err = e.b.apps.stopServices(ctx, e.app, others...); err != nil {
			return err
		}
	}
	ctx = context.WithoutCancel(ctx)
	if e.b.crash != nil && e.b.crash("built") {
		return errCrash
	}
	if err := rename(ctx, t, live, aside); err != nil {
		return errors.Join(fmt.Errorf("switch: %w; nothing changed", err), start())
	}
	if e.b.crash != nil && e.b.crash("aside") {
		return errCrash
	}
	if err := rename(ctx, t, built, live); err != nil {
		return errors.Join(fmt.Errorf("switch: %w", err), rename(ctx, t, aside, live), start())
	}
	if e.b.crash != nil && e.b.crash("switched") {
		return errCrash
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

// Recover: <db> missing and <db>_before_<id> there, it goes back. It
// waits up to a minute for the Postgres to run.
func (e *postgresEngine) Recover(ctx context.Context, id string) (bool, error) {
	t, err := e.target(ctx)
	for try := 0; err != nil && try < 30; try++ {
		time.Sleep(2 * time.Second)
		t, err = e.target(ctx)
	}
	if err != nil {
		return false, err
	}
	aside := undoName(t.database, id)
	if !dbExists(ctx, t, t.database) && dbExists(ctx, t, aside) {
		if err := rename(ctx, t, aside, t.database); err != nil {
			return false, err
		}
	}
	_, err = admin(ctx, t, `DROP DATABASE IF EXISTS :"db";`, map[string]string{"db": t.database + "_restore"})
	return dbExists(ctx, t, aside), err
}

// Check loads a copy into the throwaway database <db>_check and reads
// every row of every table, then drops it.
func (e *postgresEngine) Check(ctx context.Context, copy io.Reader) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	check := t
	check.database = t.database + "_check"
	if err := load(ctx, t, check.database, copy); err != nil {
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
	out, err := admin(ctx, t, `SELECT datname || ' ' || pg_database_size(datname) FROM pg_database WHERE starts_with(datname, :'prefix') ORDER BY datname DESC;`, map[string]string{"prefix": undoName(t.database, "")})
	undos := []protocol.Copy{}
	for _, line := range strings.Split(out, "\n") {
		name, size, _ := strings.Cut(line, " ")
		id := strings.ToUpper(strings.TrimPrefix(name, undoName(t.database, "")))
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
