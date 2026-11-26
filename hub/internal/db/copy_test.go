package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

// TestCopyBeforeMigrations copies a database that misses migrations,
// keeps only the newest copies, and leaves fresh and current databases alone.
func TestCopyBeforeMigrations(t *testing.T) {
	dir := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	path, err := CopyBeforeMigrations(database, dir, "v1.0.0")
	if err != nil || path != "" {
		t.Fatalf("fresh database: path %q, error %v; want no copy", path, err)
	}
	if err := setupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, ".", 13); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO users (id, email) VALUES ('u1', 'ada@example.com')`); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"v0.1.0", "v0.2.0", "v0.3.0"} {
		file := filepath.Join(dir, copyPrefix+old+".db")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		past := time.Now().Add(-time.Hour)
		_ = os.Chtimes(file, past, past)
	}

	path, err = CopyBeforeMigrations(database, dir, "v1.0.0-beta.3")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "backup-before-v1.0.0-beta.3.db") {
		t.Fatalf("path = %q", path)
	}
	copied, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	var email string
	if err := copied.QueryRow(`SELECT email FROM users WHERE id = 'u1'`).Scan(&email); err != nil || email != "ada@example.com" {
		t.Fatalf("copied user =%q, %v", email, err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, copyPrefix+"*.db"))
	if len(matches) != copiesKept {
		t.Fatalf("copies = %v, want %d", matches, copiesKept)
	}

	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	path, err = CopyBeforeMigrations(database, dir, "v1.0.0")
	if err != nil || path != "" {
		t.Fatalf("current database: path %q, error %v; want no copy", path, err)
	}
}

// TestPreflight migrates a copy of a database that misses migrations, while
// the hub holds it open, and leaves the database as it was.
func TestPreflight(t *testing.T) {
	dir := t.TempDir()
	connection := filepath.Join(dir, "app.db") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	if err := Preflight(connection); err != nil {
		t.Fatalf("no database yet: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("no database yet: preflight left %v", entries)
	}

	database, err := Init(connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := setupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 62); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO users (id, email) VALUES ('u1', 'ada@example.com')`); err != nil {
		t.Fatal(err)
	}
	history, err := OpenHistory(HistoryConnection(connection))
	if err != nil {
		t.Fatal(err)
	}
	if err := history.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Preflight(connection); err != nil {
		t.Fatal(err)
	}
	version, err := goose.GetDBVersion(database.DB)
	if err != nil || version != 62 {
		t.Fatalf("version after preflight = %d, %v; want 62", version, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "preflight-*")); len(matches) != 0 {
		t.Fatalf("copies left = %v", matches)
	}
}

// TestPreflightFails reports a migration that fails and removes its copy.
func TestPreflightFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "app.db")
	database, err := Init(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY); INSERT INTO sessions (id) VALUES ('s1')`); err != nil {
		t.Fatal(err)
	}

	broken := func(copied *sqlx.DB) error {
		_, err := copied.Exec(`ALTER TABLE sessions ADD COLUMN missing TEXT NOT NULL`)
		return err
	}
	if err := preflightCopy(file, broken); err == nil {
		t.Fatal("preflight passed a migration that fails")
	}
	var columns int
	if err := database.Get(&columns, `SELECT count(*) FROM pragma_table_info('sessions')`); err != nil || columns != 1 {
		t.Fatalf("columns of the database = %d, %v; want 1", columns, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "preflight-*")); len(matches) != 0 {
		t.Fatalf("copies left = %v", matches)
	}
}
