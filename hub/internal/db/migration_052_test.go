package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration052MovesTheDomains runs 052 over apps with domains in
// their JSON columns: each becomes a row, the canonical one is main, a
// domain two apps had stays with the older one, and the columns go. The
// table then takes a name once and one main domain per service.
func TestMigration052MovesTheDomains(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 51); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO users (id, email, created_at) VALUES ('ada', 'ada@example.com', $1)`, now)
	database.MustExec(`INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES ('w1', 'Ada', 'ada', $1, $1)`, now)
	database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at) VALUES ('s1', 'w1', 'server-1', $1)`, now)
	database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at, domains, canonical, service_domains)
		VALUES ('a1', 'w1', 's1', 'my-app', 'my-app', $1, '["example.com","www.example.com"]', 'example.com', '{"api":["api.example.com"]}')`, now)
	database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at, domains)
		VALUES ('a2', 'w1', 's1', 'my-blog', 'my-blog', $1, '["blog.example.com","www.example.com"]')`, now.Add(time.Hour))

	if err := goose.UpTo(database.DB, ".", 52); err != nil {
		t.Fatal(err)
	}
	type row struct {
		AppID   string `db:"app_id"`
		Service string `db:"service"`
		Name    string `db:"name"`
		Main    bool   `db:"main"`
	}
	var rows []row
	if err := database.Select(&rows, `SELECT app_id, service, name, main FROM domains ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	want := []row{
		{"a1", "", "example.com", true},
		{"a1", "", "www.example.com", false},
		{"a2", "", "blog.example.com", false},
		{"a1", "api", "api.example.com", false},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
	for _, query := range []string{`SELECT domains FROM apps`, `SELECT canonical FROM apps`, `SELECT service_domains FROM apps`} {
		if _, err := database.Exec(query); err == nil {
			t.Errorf("%s still works", query)
		}
	}

	if _, err := database.Exec(`INSERT INTO domains (app_id, name) VALUES ('a2', 'example.com')`); err == nil {
		t.Error("a second app took example.com")
	}
	if _, err := database.Exec(`UPDATE domains SET main = 1 WHERE name = 'www.example.com'`); err == nil {
		t.Error("a service got a second main domain")
	}
	if _, err := database.Exec(`INSERT INTO domains (app_id, name) VALUES ('a2', 'Shop.example.com')`); err == nil {
		t.Error("a domain with capitals went in")
	}
	database.MustExec(`DELETE FROM apps WHERE id = 'a1'`)
	var left int
	if err := database.Get(&left, `SELECT COUNT(*) FROM domains`); err != nil || left != 1 {
		t.Errorf("domains left = %d, %v, want the other app's one", left, err)
	}

	if err := goose.DownTo(database.DB, ".", 51); err != nil {
		t.Fatalf("down: %v", err)
	}
}
