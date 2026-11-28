package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestPasswordComesOnStdin: the password goes to the server on stdin,
// never on a command line, and is never printed.
func TestPasswordComesOnStdin(t *testing.T) {
	runs, stdin := fakeServer(t, "", `{"app":"shop","release":4,"addresses":["https://shop.example.com"]}`)
	var out bytes.Buffer
	if err := password(context.Background(), []string{"shop", "--user", "ada", "-s", "prod"}, strings.NewReader("open sesame\n"), &out); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, stdin); got != "open sesame" {
		t.Errorf("the server read %q, want the password without its line break", got)
	}
	if ran := readFile(t, runs); !strings.Contains(ran, "here password shop --user ada") || strings.Contains(ran, "sesame") {
		t.Errorf("ssh ran %q", ran)
	}
	if !strings.Contains(out.String(), "asks every visitor") || !strings.Contains(out.String(), "Webhooks") || strings.Contains(out.String(), "sesame") {
		t.Errorf("out = %q", out.String())
	}
}

// TestPasswordNeedsOne: without a password nothing runs.
func TestPasswordNeedsOne(t *testing.T) {
	runs, _ := fakeServer(t, "", "")
	err := password(context.Background(), []string{"shop", "-s", "prod"}, strings.NewReader("\n"), &bytes.Buffer{})
	if err == nil || readFile(t, runs) != "" {
		t.Fatalf("err = %v, ran %q: want a refusal", err, readFile(t, runs))
	}
}

// TestPasswordOff asks the server to let everyone in again.
func TestPasswordOff(t *testing.T) {
	runs, _ := fakeServer(t, "", `{"app":"shop","release":5,"addresses":[]}`)
	var out bytes.Buffer
	if err := password(context.Background(), []string{"off", "shop", "-s", "prod"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if ran := readFile(t, runs); !strings.Contains(ran, "here password shop --off") {
		t.Errorf("ssh ran %q", ran)
	}
	if !strings.Contains(out.String(), "everyone in again") {
		t.Errorf("out = %q", out.String())
	}
}

// TestSQLShowsTheRows: the query goes to the server on stdin, the rows
// come back as a table with NULL apart from an empty text, or as JSON.
func TestSQLShowsTheRows(t *testing.T) {
	answer := `{"database":"postgres-db","columns":["id","body"],"rows":[["1","a\nb"],["2",null]],"more":true,"rows_affected":0}`
	runs, stdin := fakeServer(t, "", answer)
	query := `SELECT id, body FROM notes WHERE body <> 'it''s'`
	var out bytes.Buffer
	if err := sqlCommand(context.Background(), []string{"shop", query, "--limit", "2", "-s", "prod"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, stdin); got != query {
		t.Errorf("the server read %q, want the query", got)
	}
	if ran := readFile(t, runs); !strings.Contains(ran, "here sql shop --limit 2") || strings.Contains(ran, "SELECT") || strings.Contains(ran, "--write") {
		t.Errorf("ssh ran %q", ran)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || !strings.Contains(lines[0], "id   body") || lines[1] != "1    a b" || !strings.Contains(lines[2], "NULL") || !strings.Contains(lines[3], "first 2 rows") {
		t.Errorf("out = %q", out.String())
	}
	out.Reset()
	if err := sqlCommand(context.Background(), []string{"shop", query, "-s", "prod", "--json"}, strings.NewReader(""), &out); err != nil || !strings.Contains(out.String(), `"a\nb"`) || !strings.Contains(out.String(), "null") {
		t.Errorf("json = %q, %v", out.String(), err)
	}
}

// TestSQLTables needs no query.
func TestSQLTables(t *testing.T) {
	runs, _ := fakeServer(t, "", `{"database":"/data/app.db","columns":["table"],"rows":[["notes"]],"rows_affected":0}`)
	var out bytes.Buffer
	if err := sqlCommand(context.Background(), []string{"shop", "--tables", "-s", "prod"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if ran := readFile(t, runs); !strings.Contains(ran, "here sql shop --limit 200 --tables") {
		t.Errorf("ssh ran %q", ran)
	}
	if !strings.Contains(out.String(), "notes") {
		t.Errorf("out = %q", out.String())
	}
}

// TestSQLWriteAsksFirst: without a terminal a write needs --yes. Then
// it says how many rows changed and how to undo it.
func TestSQLWriteAsksFirst(t *testing.T) {
	answer := `{"database":"postgres-db","columns":[],"rows":[],"rows_affected":2,"copy":"20261127T140700Z-482913"}`
	runs, _ := fakeServer(t, "", answer)
	statement := "UPDATE notes SET body = 'x'"
	err := sqlCommand(context.Background(), []string{"shop", statement, "--write", "-s", "prod"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--yes") || readFile(t, runs) != "" {
		t.Fatalf("err = %v, ran %q: want a question first", err, readFile(t, runs))
	}
	var out bytes.Buffer
	if err := sqlCommand(context.Background(), []string{"shop", statement, "--write", "-y", "-s", "prod"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if ran := readFile(t, runs); !strings.Contains(ran, "here sql shop --limit 200 --write") || strings.Contains(ran, "--no-copy") {
		t.Errorf("ssh ran %q", ran)
	}
	if !strings.Contains(out.String(), "2 rows changed in postgres-db of shop") || !strings.Contains(out.String(), "restore shop --to before-sql") {
		t.Errorf("out = %q", out.String())
	}
}

// TestSQLSeveralDatabases: without a terminal moin does not guess which
// database, it names the flag.
func TestSQLSeveralDatabases(t *testing.T) {
	fakeServer(t, "", `{"columns":null,"rows":null,"rows_affected":0,"choose":["/data/app.db","postgres-db"]}`)
	err := sqlCommand(context.Background(), []string{"shop", "SELECT 1", "-s", "prod"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--db /data/app.db") || !strings.Contains(err.Error(), "postgres-db") {
		t.Fatalf("err = %v, want the databases and the flag", err)
	}
}
