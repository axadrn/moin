//go:build docker

package daemon

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// The promises of `moin sql` against real Postgres, MySQL and MariaDB: a
// query answers its rows, a read changes nothing, a write is copied
// first, runs whole or not at all, and before-sql brings the data back.
// Run like the backup tests.
func TestSQL(t *testing.T) {
	postgres := protocol.Database{Path: protocol.PostgresKey("db"), Kind: protocol.DatabasePostgres, Service: "db"}
	mysql := protocol.Database{Path: protocol.MySQLKey("db"), Kind: protocol.DatabaseMySQL, Service: "db"}
	for _, engine := range []struct {
		image       string
		database    protocol.Database
		environment map[string]string
	}{
		{"postgres:17-alpine", postgres, map[string]string{"POSTGRES_USER": "app", "POSTGRES_DB": "shop", "POSTGRES_PASSWORD": "secret"}},
		{"mysql:8", mysql, map[string]string{"MYSQL_ROOT_PASSWORD": "s3cret\" x", "MYSQL_DATABASE": "shop"}},
		{"mariadb:11", mysql, map[string]string{"MARIADB_ROOT_PASSWORD": "s3cret\" x", "MARIADB_DATABASE": "shop"}},
	} {
		t.Run(engine.image, func(t *testing.T) { testSQL(t, engine.image, engine.database, engine.environment) })
	}
}

func testSQL(t *testing.T, image string, database protocol.Database, environment map[string]string) {
	app := fmt.Sprintf("sql-%d", time.Now().UnixNano()%1e6)
	compose := map[string]any{"services": map[string]any{
		"web": map[string]any{"image": "alpine:latest", "command": []string{"sleep", "infinity"}, "init": true},
		"db":  map[string]any{"image": image, "environment": environment},
	}}
	test := newBackupTest(t, app, compose, database)
	ask := func(request SQL) (SQLResult, error) {
		request.App = app
		return test.a.sql(test.ctx, request, io.Discard)
	}
	rows := func(query string) [][]string {
		t.Helper()
		result, err := ask(SQL{Query: query})
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return texts(result.Rows)
	}
	// The database takes a moment to start, MySQL two starts.
	for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(time.Second) {
		_, err := ask(SQL{Query: "SELECT 1"})
		if err == nil {
			if _, err = ask(SQL{Query: "CREATE TABLE notes (id INT PRIMARY KEY, body TEXT)", Write: true, NoCopy: true}); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
	}
	changed, err := ask(SQL{Query: "INSERT INTO notes VALUES (1, 'a, \"b\"\nc <d> & e'), (2, NULL), (3, '');", Write: true, NoCopy: true})
	if err != nil || changed.Affected != 3 || changed.Copy != "" {
		t.Fatalf("the insert: %+v %v", changed, err)
	}

	// A query answers its columns and rows, NULL apart from an empty text.
	result, err := ask(SQL{Query: "SELECT id, body FROM notes ORDER BY id"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"id", "body"}; !reflect.DeepEqual(result.Columns, want) || result.Database != database.Path {
		t.Errorf("columns = %v of %s", result.Columns, result.Database)
	}
	if want := [][]string{{"1", "a, \"b\"\nc <d> & e"}, {"2", "<nil>"}, {"3", ""}}; !reflect.DeepEqual(texts(result.Rows), want) || result.More {
		t.Errorf("rows = %q, more = %v", texts(result.Rows), result.More)
	}
	if result, err = ask(SQL{Query: "SELECT id FROM notes ORDER BY id", Limit: 2}); err != nil || len(result.Rows) != 2 || !result.More {
		t.Errorf("with a limit of 2: %q, more = %v, %v", texts(result.Rows), result.More, err)
	}
	if got := rows("SELECT id FROM notes WHERE id > 9"); len(got) != 0 {
		t.Errorf("no rows = %q", got)
	}
	if result, err = ask(SQL{Tables: true}); err != nil || len(result.Rows) != 1 || *result.Rows[0][len(result.Rows[0])-1] != "notes" {
		t.Errorf("tables = %q %v", texts(result.Rows), err)
	}

	// A read changes nothing, and says why in the database's own words.
	for _, write := range []string{"DELETE FROM notes", "UPDATE notes SET body = 'x'", "DROP TABLE notes"} {
		if _, err := ask(SQL{Query: write}); err == nil {
			t.Errorf("%s ran on a read", write)
		} else if !strings.Contains(strings.ToLower(err.Error()), "read") {
			t.Errorf("%s: the error does not say read-only: %v", write, err)
		}
	}
	if got := rows("SELECT count(*) FROM notes"); got[0][0] != "3" {
		t.Fatalf("after the refused writes: %q", got)
	}
	if _, err := ask(SQL{Query: "SELECT nope FROM notes"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("a query with a mistake: %v", err)
	}

	// A write is copied first and says how many rows it changed.
	changed, err = ask(SQL{Query: "UPDATE notes SET body = 'x' WHERE id < 3;", Write: true})
	if err != nil || changed.Affected != 2 || !protocol.ValidCopyID(changed.Copy) {
		t.Fatalf("the update: %+v %v", changed, err)
	}
	if noted, copy, err := test.a.beforeSQLCopy(app); err != nil || noted != database.Path || copy != changed.Copy {
		t.Fatalf("before-sql = %s %s %v", noted, copy, err)
	}
	// One that fails half way changes nothing.
	if _, err := ask(SQL{Query: "DELETE FROM notes; INSERT INTO nowhere VALUES (1)", Write: true, NoCopy: true}); err == nil {
		t.Fatal("a statement on a table that is not there worked")
	}
	if got, want := rows("SELECT body FROM notes ORDER BY id"), [][]string{{"x"}, {"x"}, {""}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after the writes: %q", got)
	}

	// before-sql is the data before the last copied write.
	if _, err := restoreHere(test.ctx, test.a, Restore{App: app, To: BeforeSQL}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got, want := rows("SELECT body FROM notes ORDER BY id"), [][]string{{"a, \"b\"\nc <d> & e"}, {"<nil>"}, {""}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after the restore: %q", got)
	}
}
