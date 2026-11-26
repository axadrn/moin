package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/db"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/jmoiron/sqlx"
)

// preflightHub is the settings of a hub in a directory of its own, and its
// database, migrated and held open as the running hub holds it.
func preflightHub(t *testing.T) (*config.Config, *sqlx.DB) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DBConnection: filepath.Join(dir, "app.db") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		SecretsKey:   bytes.Repeat([]byte("k"), 32),
		AppURL:       "http://localhost:8080",
	}
	database, err := db.Init(cfg.DBConnection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(database.DB); err != nil {
		t.Fatal(err)
	}
	return cfg, database
}

// preflightLeft is what a preflight left next to the database.
func preflightLeft(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(strings.Split(cfg.DBConnection, "?")[0]), "preflight-*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// A preflight without a database yet passes and makes nothing.
func TestPreflightWithoutDatabase(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBConnection: filepath.Join(dir, "app.db"), SecretsKey: bytes.Repeat([]byte("k"), 32)}
	if err := Preflight(cfg); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("preflight left %v", entries)
	}
}

// A preflight starts the hub on a copy: a migration that fails on the data
// fails it, and the database keeps its version.
func TestPreflightMigratesACopy(t *testing.T) {
	cfg, database := preflightHub(t)
	if err := Preflight(cfg); err != nil {
		t.Fatal(err)
	}
	if left := preflightLeft(t, cfg); len(left) != 0 {
		t.Fatalf("preflight left %v", left)
	}

	// 063 counts as not run while its columns are there, so it fails on
	// the copy as a migration fails on real data.
	if _, err := database.Exec(`DELETE FROM goose_db_version WHERE version_id >= 63`); err != nil {
		t.Fatal(err)
	}
	err := Preflight(cfg)
	if err == nil || !strings.Contains(err.Error(), "migrations") {
		t.Fatalf("preflight with a failing migration = %v, want it to fail", err)
	}
	var version int
	if err := database.Get(&version, `SELECT max(version_id) FROM goose_db_version`); err != nil || version != 62 {
		t.Fatalf("version after preflight = %d, %v; want 62", version, err)
	}
	if left := preflightLeft(t, cfg); len(left) != 0 {
		t.Fatalf("preflight left %v", left)
	}
}

// A preflight opens the GitHub App in the database as the start does: with
// another SECRETS_KEY it fails.
func TestPreflightOpensTheGitHubApp(t *testing.T) {
	cfg, database := preflightHub(t)
	box, err := secret.New(bytes.Repeat([]byte("o"), 32))
	if err != nil {
		t.Fatal(err)
	}
	err = repository.NewGitHubApps(database).Create(&model.GitHubApp{
		AppID: "1", Slug: "moin-hub", ClientID: "client",
		ClientSecret:  box.Seal("secret", "github_app/client_secret"),
		WebhookSecret: box.Seal("secret", "github_app/webhook_secret"),
		PrivateKey:    box.Seal("key", "github_app/private_key"),
		CreatedAt:     time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = Preflight(cfg)
	if err == nil || !strings.Contains(err.Error(), "SECRETS_KEY") {
		t.Fatalf("preflight with another SECRETS_KEY = %v, want it to fail", err)
	}
	if left := preflightLeft(t, cfg); len(left) != 0 {
		t.Fatalf("preflight left %v", left)
	}
}
