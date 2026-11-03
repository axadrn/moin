package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestOrderMigrationKeepsTheOrderByName numbers the apps and servers there
// are by name, per workspace, the order lists had before 018.
func TestOrderMigrationKeepsTheOrderByName(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "order.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := setupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, ".", 16); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO workspaces (id, name, slug) VALUES ('w1', 'Acme', 'acme')`,
		`INSERT INTO workspaces (id, name, slug) VALUES ('w2', 'Other', 'other')`,
		`INSERT INTO servers (id, workspace_id, name) VALUES ('s1', 'w1', 'web')`,
		`INSERT INTO servers (id, workspace_id, name) VALUES ('s2', 'w1', 'db')`,
		`INSERT INTO servers (id, workspace_id, name) VALUES ('s3', 'w2', 'zeta')`,
		`INSERT INTO apps (id, workspace_id, server_id, name) VALUES ('a1', 'w1', 's1', 'shop')`,
		`INSERT INTO apps (id, workspace_id, server_id, name) VALUES ('a2', 'w1', 's1', 'blog')`,
		`INSERT INTO apps (id, workspace_id, server_id, name) VALUES ('a3', 'w1', 's2', 'api')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := goose.UpTo(database, ".", 18); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]string{
		`SELECT name || '=' || position FROM apps ORDER BY name`:    "api=0,blog=1,shop=2",
		`SELECT name || '=' || position FROM servers ORDER BY name`: "db=0,web=1,zeta=0",
	} {
		rows, err := database.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatal(err)
			}
			got = append(got, row)
		}
		_ = rows.Close()
		if strings.Join(got, ",") != want {
			t.Errorf("%s = %v, want %s", query, got, want)
		}
	}
}
