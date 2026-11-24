package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration051DropsTheSecrets runs 051 over a hub with secrets: every
// sealed value is gone, the names and dates stay, and server_secrets goes
// with its server.
func TestMigration051DropsTheSecrets(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
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
	database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at, tunnel_token) VALUES ('s1', 'w1', 'server-1', $1, 'sealed-tunnel')`, now)
	database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at, instant_password) VALUES ('a1', 'w1', 's1', 'my-app', 'my-app', $1, 'sealed-password')`, now)
	database.MustExec(`INSERT INTO app_env (app_id, key, value, updated_at) VALUES ('a1', 'DATABASE_URL', 'sealed-env', $1)`, now)
	database.MustExec(`INSERT INTO registries (workspace_id, host, username, password, created_at) VALUES ('w1', 'ghcr.io', 'ada', 'sealed-registry', $1)`, now)
	database.MustExec(`INSERT INTO integrations (workspace_id, cloudflare_token, tailscale_key) VALUES ('w1', 'sealed-cloudflare', 'sealed-tailscale')`)

	if err := goose.UpTo(database.DB, ".", 51); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT value FROM app_env`, `SELECT password FROM registries`, `SELECT secret_key FROM buckets`,
		`SELECT tailscale_key FROM integrations`, `SELECT tunnel_token FROM servers`, `SELECT instant_password FROM apps`,
	} {
		if _, err := database.Exec(query); err == nil {
			t.Errorf("%s still works", query)
		}
	}
	var keys, registries int
	database.Get(&keys, `SELECT COUNT(*) FROM app_env WHERE key = 'DATABASE_URL'`)
	database.Get(&registries, `SELECT COUNT(*) FROM registries WHERE username = 'ada'`)
	var cloudflare string
	database.Get(&cloudflare, `SELECT cloudflare_token FROM integrations`)
	if keys != 1 || registries != 1 || cloudflare != "sealed-cloudflare" {
		t.Errorf("names went too: keys %d, registries %d, cloudflare %q", keys, registries, cloudflare)
	}
	database.MustExec(`INSERT INTO server_secrets (server_id, name, updated_at) VALUES ('s1', 'tunnel', $1)`, now)
	database.MustExec(`DELETE FROM apps`)
	database.MustExec(`DELETE FROM servers`)
	var secrets int
	if err := database.Get(&secrets, `SELECT COUNT(*) FROM server_secrets`); err != nil || secrets != 0 {
		t.Errorf("server_secrets = %d, %v, want them gone with their server", secrets, err)
	}

	if err := goose.DownTo(database.DB, ".", 50); err != nil {
		t.Fatalf("down: %v", err)
	}
}
