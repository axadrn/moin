package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration047SignsEveryoneOut runs 047 over a hub with passwords and
// sign-in links: the password column and the dead links are gone, every
// web session ends, email changes stay, and users get room for a GitHub
// account and a recovery key.
func TestMigration047SignsEveryoneOut(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 46); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO users (id, email, password_hash, created_at) VALUES ('ada', 'ada@example.com', 'hash', $1), ('bob', 'bob@example.com', NULL, $1)`, now)
	database.MustExec(`INSERT INTO tokens (id, user_id, type, token_hash, expires_at) VALUES
		('1', 'ada', 'magic_link', 'a', $1), ('2', 'ada', 'password_reset', 'b', $1), ('3', 'ada', 'email_change', 'c', $1)`, now.Add(time.Hour))
	database.MustExec(`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ('s1', 'ada', 'x', $1), ('s2', 'bob', 'y', $1)`, now.Add(time.Hour))

	if err := goose.UpTo(database.DB, ".", 47); err != nil {
		t.Fatal(err)
	}

	var sessions, users int
	var tokens []string
	if err := database.Get(&sessions, `SELECT COUNT(*) FROM sessions`); err != nil || sessions != 0 {
		t.Errorf("sessions = %d, %v, want none", sessions, err)
	}
	if err := database.Select(&tokens, `SELECT type FROM tokens`); err != nil || len(tokens) != 1 || tokens[0] != "email_change" {
		t.Errorf("tokens = %v, %v, want the email change", tokens, err)
	}
	if err := database.Get(&users, `SELECT COUNT(*) FROM users WHERE github_id IS NULL AND github_login = '' AND recovery_key_hash IS NULL`); err != nil || users != 2 {
		t.Errorf("users = %d, %v", users, err)
	}
	if _, err := database.Exec(`SELECT password_hash FROM users`); err == nil {
		t.Error("password_hash is still there")
	}
	database.MustExec(`UPDATE users SET github_id = 1 WHERE id = 'ada'`)
	if _, err := database.Exec(`UPDATE users SET github_id = 1 WHERE id = 'bob'`); err == nil {
		t.Error("two users share a GitHub account")
	}

	if err := goose.DownTo(database.DB, ".", 46); err != nil {
		t.Fatalf("down: %v", err)
	}
}
