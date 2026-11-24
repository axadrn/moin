package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration049AddsPasskeys runs 049 over a hub with users: a passkey
// belongs to one user, its credential id to one passkey, and it goes with
// its user. Down drops both tables.
func TestMigration049AddsPasskeys(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 48); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO users (id, email, created_at) VALUES ('ada', 'ada@example.com', $1), ('bob', 'bob@example.com', $1)`, now)

	if err := goose.UpTo(database.DB, ".", 49); err != nil {
		t.Fatal(err)
	}

	database.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p1', 'ada', x'01', '{}', 'Passkey')`)
	if _, err := database.Exec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p2', 'bob', x'01', '{}', 'Passkey')`); err == nil {
		t.Error("two passkeys share a credential id")
	}
	if _, err := database.Exec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p3', 'nobody', x'02', '{}', 'Passkey')`); err == nil {
		t.Error("a passkey without its user")
	}
	database.MustExec(`INSERT INTO passkey_ceremonies (id_hash, session_data, expires_at) VALUES ('h', '{}', $1)`, now)
	database.MustExec(`DELETE FROM users WHERE id = 'ada'`)
	var passkeys int
	if err := database.Get(&passkeys, `SELECT COUNT(*) FROM passkeys`); err != nil || passkeys != 0 {
		t.Errorf("passkeys = %d, %v, want them gone with their user", passkeys, err)
	}

	if err := goose.DownTo(database.DB, ".", 48); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := database.Exec(`SELECT 1 FROM passkeys`); err == nil {
		t.Error("passkeys is still there")
	}
}
