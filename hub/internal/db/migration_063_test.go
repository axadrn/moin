package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration063KeepsSessions runs 063 on a hub with a signed-in person:
// the session stays, confirmed at its sign-in.
func TestMigration063KeepsSessions(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 62); err != nil {
		t.Fatal(err)
	}
	signedIn := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	database.MustExec(`INSERT INTO users (id, email, created_at) VALUES ('ada', 'ada@example.com', $1)`, signedIn)
	database.MustExec(`INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at) VALUES ('s1', 'ada', 'hash', $1, $2)`, signedIn.Add(24*time.Hour), signedIn)

	if err := goose.UpTo(database.DB, ".", 63); err != nil {
		t.Fatal(err)
	}
	var session struct {
		ConfirmedAt   time.Time `db:"confirmed_at"`
		ConfirmedWith string    `db:"confirmed_with"`
	}
	if err := database.Get(&session, `SELECT confirmed_at, confirmed_with FROM sessions WHERE id = 's1'`); err != nil {
		t.Fatal(err)
	}
	if !session.ConfirmedAt.Equal(signedIn) || session.ConfirmedWith != "sign-in" {
		t.Errorf("confirmed %v with %q, want %v with sign-in", session.ConfirmedAt, session.ConfirmedWith, signedIn)
	}
}
