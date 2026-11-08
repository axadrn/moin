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
// keyed postgres-<service>, after the SQLite files; MySQL is seen and
// cannot be backed up.
func TestDatabasesFindsPostgres(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.json")
	body := `{"services":{"web":{"image":"moin-shop-web:1"},"db":{"image":"postgres:17"},"cache":{"image":"redis:7"},"legacy":{"image":"mysql:8"},"analytics":{"image":"docker.io/library/postgres:16"}}}`
	if err := os.WriteFile(composeFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newApps(dir, nil)
	found, noBackup, err := a.databases(context.Background(), composeFile)
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.Database{
		{Path: "postgres-analytics", Kind: "postgres", Service: "analytics"},
		{Path: "postgres-db", Kind: "postgres", Service: "db"},
	}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("found %+v, want %+v", found, want)
	}
	if protocol.CopyFile("x", found[0].Postgres()) != "copies/x.dump" || protocol.CopyFile("x", false) != "copies/x.db.gz" {
		t.Error("copy file endings")
	}
	if len(noBackup) != 1 || noBackup[0].Path != "mysql-legacy" {
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

// TestPgBind: params become psql variables in the places of the ? marks
// outside literals, quoted names, dollar quotes and comments; nothing a
// param holds becomes SQL.
func TestPgBind(t *testing.T) {
	sql, vars, err := pgBind(`SELECT * FROM "we?ird" WHERE a = ? AND b = '?' AND c = $$?$$ -- ?
AND d = ? /* ? */ LIMIT ? OFFSET ?`, []any{"x'); DROP TABLE users; --", nil, float64(100), "0"})
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT * FROM "we?ird" WHERE a = :'p1' AND b = '?' AND c = $$?$$ -- ?
AND d = NULL /* ? */ LIMIT :'p3' OFFSET :'p4'`
	if sql != want {
		t.Errorf("sql =\n%s\nwant\n%s", sql, want)
	}
	if vars["p1"] != "x'); DROP TABLE users; --" || vars["p3"] != "100" || vars["p4"] != "0" || len(vars) != 3 {
		t.Errorf("vars = %v", vars)
	}
	// Without params the SQL stays, jsonb operators and all.
	if sql, _, err := pgBind(`SELECT data ? 'key' FROM t`, nil); err != nil || sql != `SELECT data ? 'key' FROM t` {
		t.Errorf("no params: %q, %v", sql, err)
	}
	for _, bad := range []struct {
		sql    string
		params []any
	}{
		{`SELECT ?`, []any{"a", "b"}},
		{`SELECT 1; \! rm -rf /`, nil},
		{`SELECT 1 -- \! id`, nil},
		{`SELECT 1 /* \o /tmp/x */`, nil},
		{`\copy users to '/tmp/x'`, nil},
		{`SELECT 'open`, nil},
		{`SELECT "open`, nil},
		{`SELECT $tag$ open`, nil},
		{`SELECT ?`, []any{"a\x00b"}},
	} {
		if _, _, err := pgBind(bad.sql, bad.params); err == nil {
			t.Errorf("pgBind(%q, %v) passed", bad.sql, bad.params)
		}
	}
	// Backslashes inside literals are data, not commands.
	if _, _, err := pgBind(`SELECT E'a\'b\\c', 'x\y', $q$\!$q$, "a\b"`, nil); err != nil {
		t.Errorf("backslash in literals: %v", err)
	}
}

func TestPgMultiple(t *testing.T) {
	for sql, want := range map[string]bool{
		`SELECT 1`:                    false,
		`SELECT 1;`:                   false,
		`SELECT 1; `:                  false,
		`SELECT ';'`:                  false,
		`SELECT 1; SELECT 2`:          true,
		`SELECT 1; DELETE FROM users`: true,
		`SELECT $$;$$, ';' -- ; x`:    false,
	} {
		if got := pgMultiple(sql); got != want {
			t.Errorf("pgMultiple(%q) = %v", sql, got)
		}
	}
}

func TestPgRows(t *testing.T) {
	columns, rows, err := pgRows(`[{"id":1,"name":"Ann","tags":["a","b"],"meta":{"x":1},"big":12345678901234567890,"ok":true,"gone":null},
 {"id":2,"name":"Bo","tags":null,"meta":null,"big":1,"ok":false,"gone":null}]`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(columns, ",") != "id,name,tags,meta,big,ok,gone" {
		t.Errorf("columns %v", columns)
	}
	first := rows[0]
	if first[0].(interface{ String() string }).String() != "1" || first[1] != "Ann" || first[2] != `["a","b"]` || first[3] != `{"x":1}` || first[4].(interface{ String() string }).String() != "12345678901234567890" || first[5] != true || first[6] != nil {
		t.Errorf("row %#v", first)
	}
	if len(rows) != 2 || rows[1][2] != nil {
		t.Errorf("rows %#v", rows)
	}
	if columns, rows, err := pgRows(`[]`); err != nil || len(columns) != 0 || len(rows) != 0 {
		t.Errorf("empty: %v %v %v", columns, rows, err)
	}
}

func TestPgChanges(t *testing.T) {
	if got := pgChanges("SET\nBEGIN\nUPDATE 3\nINSERT 0 2\nDELETE 1\nCOMMIT\n"); got != 6 {
		t.Errorf("changes %d", got)
	}
}

func TestPgError(t *testing.T) {
	got := pgError("psql:<stdin>:3: ERROR:  relation \"nope\" does not exist\nLINE 1: SELECT * FROM nope\n                      ^\n")
	if !strings.HasPrefix(got, `ERROR:  relation "nope" does not exist`) {
		t.Errorf("pgError = %q", got)
	}
}

// TestServiceImage: the check of a Postgres that is down takes the image
// of its service from the live release.
func TestServiceImage(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir, func(protocol.MessageType, string, any) {})
	release := a.releaseDir("shop", 3)
	if err := os.MkdirAll(release, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "compose.json"), []byte(`{"services":{"db":{"image":"postgres:17"},"web":{"image":"moin-shop-web:3"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("3"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := a.serviceImage("shop", "db"); got != "postgres:17" {
		t.Errorf("serviceImage = %q", got)
	}
	if got := a.serviceImage("shop", "none"); got != "" {
		t.Errorf("missing service = %q", got)
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
	a := newApps(dir, nil)
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
