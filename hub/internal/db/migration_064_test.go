package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration064DeletesHubUpdateEvents runs 064: the activity of the
// hub's own updates goes, every other event stays.
func TestMigration064DeletesHubUpdateEvents(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 63); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES ('w1', 'Ada', 'ada', $1, $1)`, now)
	database.MustExec(`INSERT INTO activity_events (id, workspace_id, action, target_type, target_id, metadata, created_at) VALUES
		('e1', 'w1', 'hub.update_started', 'hub', 'hub', '{}', $1),
		('e2', 'w1', 'hub.updated', 'hub', 'hub', '{}', $1),
		('e3', 'w1', 'hub.rolled_back', 'hub', 'hub', '{}', $1),
		('e4', 'w1', 'hub.update_failed', 'hub', 'hub', '{}', $1),
		('e5', 'w1', 'hub.github_app_created', 'hub', 'hub', '{}', $1),
		('e6', 'w1', 'app.password_set', 'app', 'blog', '{}', $1)`, now)

	if err := goose.UpTo(database.DB, ".", 64); err != nil {
		t.Fatal(err)
	}
	var left []string
	if err := database.Select(&left, `SELECT id FROM activity_events ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 || left[0] != "e5" || left[1] != "e6" {
		t.Errorf("events left %v, want [e5 e6]", left)
	}
	if err := goose.DownTo(database.DB, ".", 63); err != nil {
		t.Fatal(err)
	}
}
