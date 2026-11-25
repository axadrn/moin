package db_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration061LeavesNoOldSecretInTheFile runs 051 over a hub with
// sealed secrets, then 061: no old value is left in the database file or
// its write-ahead log.
func TestMigration061LeavesNoOldSecretInTheFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hub.db")
	database, err := db.Init(file + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 50); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO users (id, email, created_at) VALUES ('ada', 'ada@example.com', $1)`, now)
	database.MustExec(`INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES ('w1', 'Ada', 'ada', $1, $1)`, now)
	database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at, tunnel_token) VALUES ('s1', 'w1', 'server-1', $1, 'sealed-tunnel-secret')`, now)
	database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at) VALUES ('a1', 'w1', 's1', 'my-app', 'my-app', $1)`, now)
	database.MustExec(`INSERT INTO app_env (app_id, key, value, updated_at) VALUES ('a1', 'DATABASE_URL', 'sealed-env-secret', $1)`, now)
	database.MustExec(`INSERT INTO registries (workspace_id, host, username, password, created_at) VALUES ('w1', 'ghcr.io', 'ada', 'sealed-registry-secret', $1)`, now)
	database.MustExec(`INSERT INTO integrations (workspace_id, tailscale_key) VALUES ('w1', 'sealed-tailscale-secret')`)
	if err := goose.UpTo(database.DB, ".", 60); err != nil {
		t.Fatal(err)
	}

	if err := goose.UpTo(database.DB, ".", 61); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, file + "-wal"} {
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(content, []byte("-secret")) {
			t.Errorf("%s still holds an old secret", filepath.Base(path))
		}
	}
	if err := goose.DownTo(database.DB, ".", 60); err != nil {
		t.Fatalf("down: %v", err)
	}
}
