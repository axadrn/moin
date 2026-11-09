package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/jmoiron/sqlx"
)

const databaseOptions = "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"

// migrated holds the bytes of a fully migrated database. It is built once per
// test binary, because running every migration for every test took most of
// the time of the tests.
var migrated = sync.OnceValues(migratedDatabase)

// NewDatabase gives the test its own database, a copy of the migrated one.
func NewDatabase(t testing.TB) *sqlx.DB {
	t.Helper()
	file, err := migrated()
	if err != nil {
		t.Fatalf("build the migrated test database: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, file, 0o600); err != nil {
		t.Fatalf("copy the migrated test database: %v", err)
	}
	database, err := db.Init(path + databaseOptions)
	if err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func migratedDatabase() ([]byte, error) {
	dir, err := os.MkdirTemp("", "moin-test-database")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "migrated.db")
	database, err := db.Init(path + databaseOptions)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database.DB); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// Tests keep the history in the same file; production has history.db.
	if err := db.CreateHistory(database); err != nil {
		return nil, fmt.Errorf("create the history tables: %w", err)
	}
	// Closing the last connection writes everything into the one file.
	if err := database.Close(); err != nil {
		return nil, fmt.Errorf("close: %w", err)
	}
	return os.ReadFile(path)
}
