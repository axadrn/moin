package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration058MakesCITokensAndConnections runs 058 over personal and
// device tokens. A personal token becomes a CI token with its hash, so it
// keeps working: read stays read, read and write becomes read, deploy and
// env. A device token of `moin login` ends, and so does a pending login.
// Down brings the CI tokens back as personal tokens.
func TestMigration058MakesCITokensAndConnections(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 57); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(30 * 24 * time.Hour)
	database.MustExec(`INSERT INTO users (id, email, name, created_at) VALUES ('ada', 'ada@example.com', 'Ada', $1)`, now)
	database.MustExec(`INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES ('acme', 'acme', 'acme', $1, $1)`, now)
	for _, token := range []struct {
		id, scopes, kind string
		expires          *time.Time
	}{
		{"reader", "read", "personal", nil},
		{"writer", "read,write", "personal", &expires},
		{"laptop", "read,write", "device", &expires},
	} {
		database.MustExec(`INSERT INTO api_tokens (id, user_id, workspace_id, name, token_hash, prefix, scopes, kind, last_used_at, expires_at, created_at)
			VALUES ($1, 'ada', 'acme', $1, 'hash-' || $1, 'prefix', $2, $3, $4, $5, $4)`, token.id, token.scopes, token.kind, now, token.expires)
	}
	database.MustExec(`INSERT INTO device_logins (id, device_code_hash, user_code_hash, hostname, status, interval_seconds, expires_at, created_at)
		VALUES ('pending', 'device', 'user', 'host-1', 'pending', 5, $1, $1)`, now.Add(10*time.Minute))

	if err := goose.UpTo(database.DB, ".", 58); err != nil {
		t.Fatal(err)
	}

	var rows []struct {
		ID        string     `db:"id"`
		Rights    string     `db:"rights"`
		TokenHash string     `db:"token_hash"`
		ExpiresAt *time.Time `db:"expires_at"`
		LastUsed  *time.Time `db:"last_used_at"`
	}
	if err := database.Select(&rows, `SELECT id, rights, token_hash, expires_at, last_used_at FROM ci_tokens ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "reader" || rows[0].Rights != "read" || rows[0].TokenHash != "hash-reader" || rows[0].ExpiresAt != nil ||
		rows[1].ID != "writer" || rows[1].Rights != "read,deploy,env" || rows[1].ExpiresAt == nil || !rows[1].ExpiresAt.Equal(expires) || rows[1].LastUsed == nil {
		t.Fatalf("ci tokens = %+v", rows)
	}
	for _, query := range []string{`SELECT COUNT(*) FROM device_logins`, `SELECT COUNT(*) FROM connections`, `SELECT COUNT(*) FROM connection_tokens`} {
		var count int
		if err := database.Get(&count, query); err != nil || count != 0 {
			t.Fatalf("%s = %d, %v, want 0", query, count, err)
		}
	}
	var tables int
	if err := database.Get(&tables, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'api_tokens'`); err != nil || tables != 0 {
		t.Fatalf("api_tokens is still there: %d, %v", tables, err)
	}

	if err := goose.DownTo(database.DB, ".", 57); err != nil {
		t.Fatal(err)
	}
	var scopes []string
	if err := database.Select(&scopes, `SELECT scopes || ':' || kind FROM api_tokens ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || scopes[0] != "read:personal" || scopes[1] != "read,write:personal" {
		t.Fatalf("api tokens after down = %v", scopes)
	}
}
