package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration062SaysPassword runs 062: the app's htpasswd line moves to
// the column password, a deploy that had protected says password, and so
// do the activity events. Down gives the old words back.
func TestMigration062SaysPassword(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 61); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO users (id, email, created_at) VALUES ('ada', 'ada@example.com', $1)`, now)
	database.MustExec(`INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES ('w1', 'Ada', 'ada', $1, $1)`, now)
	database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at) VALUES ('s1', 'w1', 'server-1', $1)`, now)
	database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at, protect) VALUES ('a1', 'w1', 's1', 'blog', 'blog', $1, 'moin:$2a$10$hash')`, now)
	database.MustExec(`INSERT INTO deploys (id, app_id, number, status, detected) VALUES
		('d1', 'a1', 1, 'succeeded', '{"kind":"go","protected":true}'),
		('d2', 'a1', 2, 'succeeded', '{"kind":"go"}'),
		('d3', 'a1', 3, 'failed', '')`)
	database.MustExec(`INSERT INTO activity_events (id, workspace_id, action, target_type, target_id, metadata, created_at) VALUES
		('e1', 'w1', 'app.protected', 'app', 'blog', '{}', $1),
		('e2', 'w1', 'app.unprotected', 'app', 'blog', '{}', $1)`, now)

	if err := goose.UpTo(database.DB, ".", 62); err != nil {
		t.Fatal(err)
	}
	var password string
	if err := database.Get(&password, `SELECT password FROM apps WHERE id = 'a1'`); err != nil || password != "moin:$2a$10$hash" {
		t.Errorf("password %q, %v", password, err)
	}
	for id, want := range map[string]string{"d1": `{"kind":"go","password":true}`, "d2": `{"kind":"go"}`, "d3": ""} {
		var detected string
		if err := database.Get(&detected, `SELECT detected FROM deploys WHERE id = $1`, id); err != nil || detected != want {
			t.Errorf("%s: detected %q, %v, want %q", id, detected, err, want)
		}
	}
	for id, want := range map[string]string{"e1": "app.password_set", "e2": "app.password_removed"} {
		var action string
		if err := database.Get(&action, `SELECT action FROM activity_events WHERE id = $1`, id); err != nil || action != want {
			t.Errorf("%s: action %q, %v, want %q", id, action, err, want)
		}
	}

	if err := goose.DownTo(database.DB, ".", 61); err != nil {
		t.Fatal(err)
	}
	var detected, action string
	if err := database.Get(&detected, `SELECT detected FROM deploys WHERE id = 'd1'`); err != nil || detected != `{"kind":"go","protected":true}` {
		t.Errorf("down: detected %q, %v", detected, err)
	}
	if err := database.Get(&action, `SELECT action FROM activity_events WHERE id = 'e1'`); err != nil || action != "app.protected" {
		t.Errorf("down: action %q, %v", action, err)
	}
	if err := database.Get(&password, `SELECT protect FROM apps WHERE id = 'a1'`); err != nil || password != "moin:$2a$10$hash" {
		t.Errorf("down: protect %q, %v", password, err)
	}
}
