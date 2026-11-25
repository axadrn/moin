package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration053KeepsWhatTheFileSet runs 053 over three apps: one whose
// live deploy took its domains and instant address from compose.yml, one
// whose older deploy did, and one without. The first gets what its live
// deploy served as its domains in moin, with the canonical one as main, and
// the instant address of the file; the others keep theirs.
func TestMigration053KeepsWhatTheFileSet(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 52); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO users (id, email, created_at) VALUES ('ada', 'ada@example.com', $1)`, now)
	database.MustExec(`INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES ('w1', 'Ada', 'ada', $1, $1)`, now)
	database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at) VALUES ('s1', 'w1', 'server-1', $1)`, now)
	for _, app := range []string{"a1", "a2", "a3"} {
		database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at) VALUES ($1, 'w1', 's1', $1, $1, $2)`, app, now)
	}
	database.MustExec(`INSERT INTO domains (app_id, service, name, main) VALUES ('a1', '', 'old.example.com', 1), ('a2', '', 'blog.example.com', 0), ('a3', '', 'docs.example.com', 0)`)
	served := `{"kind":"compose","service":"web","domains":["shop.example.com","www.shop.example.com"],"canonical":"shop.example.com","service_domains":{"api":["api.shop.example.com"]},"config":{"service_domains":{"web":["shop.example.com","www.shop.example.com"],"api":["api.shop.example.com"]},"canonical":"shop.example.com","instant":"off"}}`
	database.MustExec(`INSERT INTO deploys (id, app_id, number, status, detected) VALUES
		('d1', 'a1', 1, 'succeeded', '{"kind":"compose","service":"web","domains":["old.example.com"]}'),
		('d2', 'a1', 2, 'succeeded', $1),
		('d3', 'a1', 3, 'failed', ''),
		('d4', 'a2', 1, 'succeeded', $1),
		('d5', 'a2', 2, 'succeeded', '{"kind":"compose","service":"web","domains":["blog.example.com"]}'),
		('d6', 'a3', 1, 'succeeded', '{"kind":"compose","service":"web","config":{"port":3000}}')`, served)

	if err := goose.UpTo(database.DB, ".", 53); err != nil {
		t.Fatal(err)
	}
	type row struct {
		AppID   string `db:"app_id"`
		Service string `db:"service"`
		Name    string `db:"name"`
		Main    bool   `db:"main"`
	}
	var rows []row
	if err := database.Select(&rows, `SELECT app_id, service, name, main FROM domains ORDER BY app_id, service, name`); err != nil {
		t.Fatal(err)
	}
	want := []row{
		{"a1", "", "shop.example.com", true},
		{"a1", "", "www.shop.example.com", false},
		{"a1", "api", "api.shop.example.com", false},
		{"a2", "", "blog.example.com", false},
		{"a3", "", "docs.example.com", false},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
	instants := map[string]string{}
	var apps []struct {
		ID      string `db:"id"`
		Instant string `db:"instant"`
	}
	if err := database.Select(&apps, `SELECT id, instant FROM apps`); err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		instants[app.ID] = app.Instant
	}
	if instants["a1"] != "off" || instants["a2"] != "sslip.io" || instants["a3"] != "sslip.io" {
		t.Errorf("instant = %v, want off only for the app whose live deploy set it", instants)
	}

	if err := goose.DownTo(database.DB, ".", 52); err != nil {
		t.Fatalf("down: %v", err)
	}
}
