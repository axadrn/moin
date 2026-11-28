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

// sqliteNotes makes a SQLite file with three notes, one without a body.
func sqliteNotes(t *testing.T) protocol.Database {
	t.Helper()
	folder := t.TempDir()
	file := filepath.Join(folder, "app.db")
	db, err := sql.Open("sqlite", "file:"+file)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT, at DATETIME); INSERT INTO notes (body, at) VALUES ('a', '2026-11-27 14:07:00'), (NULL, NULL), ('', NULL);`); err != nil {
		t.Fatal(err)
	}
	return protocol.Database{Path: "/data/app.db", HostPath: file, HostFolder: folder}
}

// texts writes rows for a comparison: NULL as <nil>.
func texts(rows [][]*string) [][]string {
	out := [][]string{}
	for _, row := range rows {
		cells := []string{}
		for _, value := range row {
			if value == nil {
				cells = append(cells, "<nil>")
			} else {
				cells = append(cells, *value)
			}
		}
		out = append(out, cells)
	}
	return out
}

// TestSQLiteRead: the rows come back as text, NULL apart from an empty
// text, at most the limit, and a write is refused.
func TestSQLiteRead(t *testing.T) {
	ctx := context.Background()
	database := sqliteNotes(t)
	result, err := sqliteRead(ctx, database, `SELECT id, body FROM notes ORDER BY id`, 200)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"id", "body"}; !reflect.DeepEqual(result.Columns, want) {
		t.Errorf("columns = %v", result.Columns)
	}
	if want := [][]string{{"1", "a"}, {"2", "<nil>"}, {"3", ""}}; !reflect.DeepEqual(texts(result.Rows), want) || result.More {
		t.Errorf("rows = %v, more = %v", texts(result.Rows), result.More)
	}

	result, err = sqliteRead(ctx, database, `SELECT id FROM notes ORDER BY id`, 2)
	if err != nil || len(result.Rows) != 2 || !result.More {
		t.Errorf("with a limit of 2: %v, more = %v, %v", texts(result.Rows), result.More, err)
	}
	result, err = sqliteRead(ctx, database, `SELECT id FROM notes ORDER BY id`, 3)
	if err != nil || len(result.Rows) != 3 || result.More {
		t.Errorf("with a limit of 3: %v, more = %v, %v", texts(result.Rows), result.More, err)
	}

	for _, write := range []string{`DELETE FROM notes`, `SELECT 1; DELETE FROM notes`, `DROP TABLE notes`} {
		if _, err := sqliteRead(ctx, database, write, 200); err == nil {
			t.Errorf("%s ran on a read", write)
		}
	}
	result, err = sqliteRead(ctx, database, `SELECT count(*) FROM notes`, 200)
	if err != nil || *result.Rows[0][0] != "3" {
		t.Fatalf("after the refused writes: %v %v", texts(result.Rows), err)
	}

	tables, err := sqliteRead(ctx, database, tablesQuery(database), 200)
	if err != nil || !reflect.DeepEqual(texts(tables.Rows), [][]string{{"notes"}}) {
		t.Errorf("tables = %v %v", texts(tables.Rows), err)
	}
}

// TestSQLiteWrite: a statement says how many rows it changed, and one
// that fails changes nothing.
func TestSQLiteWrite(t *testing.T) {
	ctx := context.Background()
	database := sqliteNotes(t)
	affected, err := sqliteWrite(ctx, database, `UPDATE notes SET body = 'x' WHERE id < 3`)
	if err != nil || affected != 2 {
		t.Fatalf("affected = %d, %v", affected, err)
	}
	if _, err := sqliteWrite(ctx, database, `DELETE FROM notes; INSERT INTO nowhere VALUES (1)`); err == nil {
		t.Fatal("a statement on a table that is not there worked")
	}
	result, err := sqliteRead(ctx, database, `SELECT body FROM notes ORDER BY id`, 200)
	if want := [][]string{{"x"}, {"x"}, {""}}; err != nil || !reflect.DeepEqual(texts(result.Rows), want) {
		t.Fatalf("rows = %v %v", texts(result.Rows), err)
	}
}

// TestReadPsql reads what psql prints: commas, quotes and line breaks in
// a value, its mark for NULL, and an empty text alone in its row.
func TestReadPsql(t *testing.T) {
	out := "id" + pgField + "body" + pgRow + "1" + pgField + "a, \"b\"\nc" + pgRow + "2" + pgField + pgNull + pgRow + "3" + pgField + "\n"
	result := readPsql([]byte(out), 200, false)
	if want := []string{"id", "body"}; !reflect.DeepEqual(result.Columns, want) {
		t.Errorf("columns = %q", result.Columns)
	}
	if want := [][]string{{"1", "a, \"b\"\nc"}, {"2", "<nil>"}, {"3", ""}}; !reflect.DeepEqual(texts(result.Rows), want) || result.More {
		t.Errorf("rows = %q, more = %v", texts(result.Rows), result.More)
	}
	if result = readPsql([]byte(out), 2, false); len(result.Rows) != 2 || !result.More {
		t.Errorf("with a limit of 2: %q, more = %v", texts(result.Rows), result.More)
	}
	// One column of empty texts is still three rows.
	result = readPsql([]byte("body"+pgRow+pgRow+pgRow+"\n"), 200, false)
	if want := [][]string{{""}, {""}, {""}}; !reflect.DeepEqual(texts(result.Rows), want) {
		t.Errorf("empty texts = %q", texts(result.Rows))
	}
	// Cut in the middle of a row: the rows before it count.
	result = readPsql([]byte("id"+pgField+"body"+pgRow+"1"+pgField+"a"+pgRow+"2"+pgField+"b"), 200, true)
	if len(result.Rows) != 1 || !result.More {
		t.Errorf("a cut answer: %q, more = %v", texts(result.Rows), result.More)
	}
	// A statement that answers nothing.
	if result = readPsql(nil, 200, false); len(result.Rows) != 0 || result.Columns == nil {
		t.Errorf("no answer: %+v", result)
	}
}

// TestReadXML reads what mysql --xml prints.
func TestReadXML(t *testing.T) {
	out := `<?xml version="1.0"?>

<resultset statement="SELECT id, body FROM notes" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <row>
	<field name="id">1</field>
	<field name="body">a &lt;b&gt; &amp; c</field>
  </row>

  <row>
	<field name="id">2</field>
	<field name="body" xsi:nil="true" />
  </row>

  <row>
	<field name="id">3</field>
	<field name="body"></field>
  </row>
</resultset>
`
	result, err := readXML([]byte(out), 200, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"id", "body"}; !reflect.DeepEqual(result.Columns, want) {
		t.Errorf("columns = %v", result.Columns)
	}
	if want := [][]string{{"1", "a <b> & c"}, {"2", "<nil>"}, {"3", ""}}; !reflect.DeepEqual(texts(result.Rows), want) || result.More {
		t.Errorf("rows = %q, more = %v", texts(result.Rows), result.More)
	}
	if result, _ = readXML([]byte(out), 1, false); len(result.Rows) != 1 || !result.More {
		t.Errorf("with a limit of 1: %q, more = %v", texts(result.Rows), result.More)
	}
	cut := out[:strings.Index(out, `<field name="body" xsi`)]
	if result, err = readXML([]byte(cut), 200, true); err != nil || len(result.Rows) != 1 || !result.More {
		t.Errorf("a cut answer: %q, more = %v, %v", texts(result.Rows), result.More, err)
	}
}

func TestPgAffected(t *testing.T) {
	if got := pgAffected("UPDATE 3\nINSERT 0 2\nDELETE 1\nCREATE TABLE\nSELECT 9\n"); got != 6 {
		t.Errorf("affected = %d, want 6", got)
	}
}

// TestSQLDatabase: the only database needs no name, several are offered
// to choose from, and one moin cannot ask is left out.
func TestSQLDatabase(t *testing.T) {
	file := protocol.Database{Path: "/data/app.db", HostPath: "/x/app.db"}
	postgres := protocol.Database{Path: "postgres-db", Kind: protocol.DatabasePostgres, Service: "db"}
	other := protocol.Database{Path: "mongo-db", Kind: "mongo", Service: "mongo"}
	a := newApps(t.TempDir())
	with := func(databases ...protocol.Database) {
		t.Helper()
		if err := os.MkdirAll(a.releaseDir("shop", 1), 0o700); err != nil {
			t.Fatal(err)
		}
		saveJSON(filepath.Join(a.releaseDir("shop", 1), "detected.json"), protocol.Detected{Databases: databases})
		if err := os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	with(file, other)
	if got, choose, err := a.sqlDatabase("shop", ""); err != nil || got != file || choose != nil {
		t.Errorf("the only one: %v %v %v", got, choose, err)
	}
	if _, _, err := a.sqlDatabase("shop", "mongo-db"); err == nil {
		t.Error("a database that speaks no SQL was taken")
	}
	if _, _, err := a.sqlDatabase("shop", "nope"); err == nil {
		t.Error("a database that is not there was found")
	}

	with(file, postgres, other)
	if got, _, err := a.sqlDatabase("shop", "postgres-db"); err != nil || got != postgres {
		t.Errorf("by name: %v %v", got, err)
	}
	if got, _, err := a.sqlDatabase("shop", "postgres-db/reports"); err != nil || got.Name != "reports" || !got.Postgres() {
		t.Errorf("another database of the Postgres: %v %v", got, err)
	}
	_, choose, err := a.sqlDatabase("shop", "")
	if want := []string{"/data/app.db", "postgres-db"}; err != nil || !reflect.DeepEqual(choose, want) {
		t.Errorf("several: %v %v", choose, err)
	}

	with(other)
	if _, _, err := a.sqlDatabase("shop", ""); err == nil {
		t.Error("an app without a SQL database has one")
	}
}

// TestBeforeSQLNote: restore --to before-sql finds the copy the last
// write took, and its database.
func TestBeforeSQLNote(t *testing.T) {
	a := newApps(t.TempDir())
	if _, _, err := a.beforeSQLCopy("shop"); err == nil {
		t.Fatal("an app without a write has a before-sql")
	}
	note := sqlNote{Database: "postgres-db", Copy: "20261127T140700Z-482913"}
	if err := writeState(a.beforeSQLFile("shop"), note); err != nil {
		t.Fatal(err)
	}
	database, copy, err := a.beforeSQLCopy("shop")
	if err != nil || database != note.Database || copy != note.Copy {
		t.Fatalf("the note = %s %s %v", database, copy, err)
	}
}
