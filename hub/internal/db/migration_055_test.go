package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration055MakesOnePassword runs 055 over five apps. The one whose
// instant address had the password moin made, and no domains, keeps it as
// its own. One with domains, one whose instant address was open and one
// without a hash stay off; one with its own password keeps it. The columns
// of the instant password are gone.
func TestMigration055MakesOnePassword(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 54); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO users (id, email, created_at) VALUES ('ada', 'ada@example.com', $1)`, now)
	database.MustExec(`INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES ('w1', 'Ada', 'ada', $1, $1)`, now)
	database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at) VALUES ('s1', 'w1', 'server-1', $1)`, now)
	for _, app := range []struct {
		id, protect, hash string
		open              bool
	}{
		{"idea", "", "$2a$10$idea", false},
		{"shop", "", "$2a$10$shop", false},
		{"open", "", "$2a$10$open", true},
		{"old", "", "", false},
		{"own", "me:$2a$10$own", "$2a$10$unused", false},
	} {
		database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at, protect, instant_password_hash, instant_open) VALUES ($1, 'w1', 's1', $1, $1, $2, $3, $4, $5)`, app.id, now, app.protect, app.hash, app.open)
	}
	database.MustExec(`INSERT INTO domains (app_id, service, name) VALUES ('shop', '', 'shop.example.com')`)

	if err := goose.UpTo(database.DB, ".", 55); err != nil {
		t.Fatal(err)
	}
	for app, want := range map[string]string{
		"idea": "moin:$2a$10$idea",
		"shop": "",
		"open": "",
		"old":  "",
		"own":  "me:$2a$10$own",
	} {
		var protect string
		if err := database.Get(&protect, `SELECT protect FROM apps WHERE id = $1`, app); err != nil || protect != want {
			t.Errorf("%s: protect %q, %v, want %q", app, protect, err, want)
		}
	}
	for _, query := range []string{`SELECT instant_open FROM apps`, `SELECT instant_password_hash FROM apps`} {
		if _, err := database.Exec(query); err == nil {
			t.Errorf("%s still works", query)
		}
	}

	if err := goose.DownTo(database.DB, ".", 54); err != nil {
		t.Fatalf("down: %v", err)
	}
}
