package daemon

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

func TestPostgresImage(t *testing.T) {
	for image, want := range map[string]bool{
		"postgres":                          true,
		"postgres:17":                       true,
		"postgres:17-alpine":                true,
		"docker.io/library/postgres:16":     true,
		"postgres@sha256:abc":               true,
		"postgis/postgis:17-3.5":            true,
		"pgvector/pgvector:pg17":            true,
		"timescale/timescaledb:latest-pg17": true,
		"timescale/timescaledb-ha:pg17":     true,
		"registry:5000/postgres:17":         false,
		"ghcr.io/me/postgres-exporter:1":    false,
		"redis:7":                           false,
		"myapp/postgres":                    false,
		"":                                  false,
	} {
		if got := postgresImage(image); got != want {
			t.Errorf("postgresImage(%q) = %v, want %v", image, got, want)
		}
	}
}

// TestDatabasesFindsPostgres: a service that runs Postgres is a database,
// keyed postgres-<service>, after the SQLite files, and so is one that
// runs MySQL or MariaDB, keyed mysql-<service>, or MongoDB, keyed
// mongo-<service>.
func TestDatabasesFindsPostgres(t *testing.T) {
	dir := t.TempDir()
	composeJSON := filepath.Join(dir, "compose.json")
	body := `{"services":{"web":{"image":"moin-shop-web:1"},"db":{"image":"postgres:17"},"cache":{"image":"redis:7"},"legacy":{"image":"mysql:8"},"wiki":{"image":"docker.io/library/mariadb:11"},"docs":{"image":"mongo:8"},"analytics":{"image":"docker.io/library/postgres:16"}}}`
	if err := os.WriteFile(composeJSON, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newApps(dir)
	found, noBackup, err := a.scanDatabases(context.Background(), composeJSON)
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.Database{
		{Path: "postgres-analytics", Kind: "postgres", Service: "analytics"},
		{Path: "postgres-db", Kind: "postgres", Service: "db"},
		{Path: "mongo-docs", Kind: "mongo", Service: "docs"},
		{Path: "mysql-legacy", Kind: "mysql", Service: "legacy"},
		{Path: "mysql-wiki", Kind: "mysql", Service: "wiki"},
	}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("found %+v, want %+v", found, want)
	}
	if protocol.CopyFile("x", found[0].Kind, false) != "copies/x.dump.age" || protocol.CopyFile("x", found[3].Kind, false) != "copies/x.sql.gz.age" || protocol.CopyFile("x", found[2].Kind, false) != "copies/x.archive.gz.age" || protocol.CopyFile("x", "", true) != "copies/x.db.gz" {
		t.Error("copy file endings")
	}
	if len(noBackup) != 0 {
		t.Errorf("cannot back up %+v", noBackup)
	}
}

func TestPgNames(t *testing.T) {
	for _, test := range []struct {
		env            []string
		user, database string
	}{
		{nil, "postgres", "postgres"},
		{[]string{"POSTGRES_USER=umami"}, "umami", "umami"},
		{[]string{"POSTGRES_USER=app", "POSTGRES_DB=shop", "PATH=/usr/bin"}, "app", "shop"},
		{[]string{"POSTGRES_DB=shop"}, "postgres", "shop"},
	} {
		user, database := pgNames(test.env)
		if user != test.user || database != test.database {
			t.Errorf("%v: %s/%s, want %s/%s", test.env, user, database, test.user, test.database)
		}
	}
}

func TestPgError(t *testing.T) {
	got := pgError("psql:<stdin>:3: ERROR:  relation \"nope\" does not exist\nLINE 1: SELECT * FROM nope\n                      ^\n")
	if !strings.HasPrefix(got, `ERROR:  relation "nope" does not exist`) {
		t.Errorf("pgError = %q", got)
	}
	// What quotes the app's data stays out: the row, the key, the command.
	got = pgError(`pg_restore: error: could not execute query: ERROR:  duplicate key value violates unique constraint "users_email_key"
DETAIL:  Key (email)=(ada@example.com) already exists.
CONTEXT:  COPY users, line 2: "7	ada@example.com"
Command was: COPY public.users (id, email) FROM stdin;
7	ada@example.com
pg_restore: warning: errors ignored on restore: 1
`)
	want := `pg_restore: error: could not execute query: ERROR:  duplicate key value violates unique constraint "users_email_key"; pg_restore: warning: errors ignored on restore: 1`
	if got != want {
		t.Errorf("pgError = %q", got)
	}
}

// A Postgres data folder under an image moin does not know is seen, and
// so is a SQLite file an app creates after its deploy. What was found
// stays when its folder is gone for a moment.
func TestFindNewDatabases(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	pg := filepath.Join(dir, "pg", "18", "docker")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pg, 0o700); err != nil {
		t.Fatal(err)
	}
	a := newApps(dir)
	release := a.releaseDir("shop", 1)
	if err := os.MkdirAll(release, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	compose := `{"services":{"web":{"image":"web:1","volumes":[{"type":"bind","source":"` + data + `","target":"/data"}]},` +
		`"db":{"image":"registry.example/pg:17","volumes":[{"type":"bind","source":"` + filepath.Join(dir, "pg") + `","target":"/var/lib/postgresql"}]}}}`
	if err := os.WriteFile(filepath.Join(release, "compose.json"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "detected.json"), []byte(`{"kind":"compose","databases":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, changed := a.findNewDatabases(ctx, "shop"); changed {
		t.Fatal("nothing is there yet")
	}
	file := filepath.Join(data, "app.db")
	db, err := sql.Open("sqlite", "file:"+file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t (x)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := os.WriteFile(filepath.Join(pg, "PG_VERSION"), []byte("18"), 0o600); err != nil {
		t.Fatal(err)
	}
	detected, changed := a.findNewDatabases(ctx, "shop")
	if !changed || len(detected.Databases) != 1 || detected.Databases[0].HostPath != file {
		t.Fatalf("the new SQLite file: %v %+v", changed, detected.Databases)
	}
	if len(detected.NoBackup) != 1 || detected.NoBackup[0].Path != "postgres-db" {
		t.Fatalf("the unknown Postgres: %+v", detected.NoBackup)
	}
	if _, changed := a.findNewDatabases(ctx, "shop"); changed {
		t.Fatal("the second search found something new")
	}
	if err := os.RemoveAll(data); err != nil {
		t.Fatal(err)
	}
	if detected, _ := a.findNewDatabases(ctx, "shop"); len(detected.Databases) != 1 {
		t.Fatalf("a folder gone for a moment ended its backups: %+v", detected.Databases)
	}
}
