package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration053DomainsPerApp runs 053 over saved domains: they stay,
// and a name is unique per app, not in the whole hub.
func TestMigration053DomainsPerApp(t *testing.T) {
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
	database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at) VALUES ('a1', 'w1', 's1', 'my-app', 'my-app', $1)`, now)
	database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at) VALUES ('a2', 'w1', 's1', 'my-blog', 'my-blog', $1)`, now)
	database.MustExec(`INSERT INTO domains (app_id, service, name, main) VALUES ('a1', '', 'example.com', 1), ('a1', 'api', 'api.example.com', 0)`)

	if err := goose.UpTo(database.DB, ".", 53); err != nil {
		t.Fatal(err)
	}
	var kept int
	if err := database.Get(&kept, `SELECT COUNT(*) FROM domains WHERE app_id = 'a1'`); err != nil || kept != 2 {
		t.Errorf("kept = %d, %v, want both rows", kept, err)
	}
	if _, err := database.Exec(`INSERT INTO domains (app_id, name) VALUES ('a2', 'example.com')`); err != nil {
		t.Errorf("a second app may set example.com too: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO domains (app_id, service, name) VALUES ('a1', 'web', 'example.com')`); err == nil {
		t.Error("one app took example.com twice")
	}
	if _, err := database.Exec(`INSERT INTO domains (app_id, name, main) VALUES ('a1', 'www.example.com', 1)`); err == nil {
		t.Error("a service got a second main domain")
	}
	database.MustExec(`DELETE FROM apps WHERE id = 'a1'`)
	var left int
	if err := database.Get(&left, `SELECT COUNT(*) FROM domains`); err != nil || left != 1 {
		t.Errorf("domains left = %d, %v, want the other app's one", left, err)
	}

	if err := goose.DownTo(database.DB, ".", 52); err != nil {
		t.Fatalf("down: %v", err)
	}
}
