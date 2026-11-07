package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestActivityMigrationKeepsOldRows fills the new activity columns of rows
// written before 014 from their metadata, user and app.
func TestActivityMigrationKeepsOldRows(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := setupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, ".", 13); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO users (id, email) VALUES ('u1', 'ada@example.com')`,
		`INSERT INTO workspaces (id, name, slug) VALUES ('w1', 'Acme', 'acme')`,
		`INSERT INTO servers (id, workspace_id, name) VALUES ('s1', 'w1', 'web-1')`,
		`INSERT INTO apps (id, workspace_id, server_id, name) VALUES ('a1', 'w1', 's1', 'blog')`,
		`INSERT INTO workspace_audit_events (id, workspace_id, actor_user_id, action, target_type, target_id, metadata, created_at)
		 VALUES ('e1', 'w1', 'u1', 'app.deployed', 'app', 'blog', '{"via":"api","number":"3"}', '2026-01-02 03:04:05.123456789+00:00')`,
		`INSERT INTO workspace_audit_events (id, workspace_id, actor_user_id, action, target_type, target_id, metadata, created_at)
		 VALUES ('e2', 'w1', 'u1', 'data.sql', 'app', 'a1', '{"via":"mcp"}', '2026-01-02 03:04:06+00:00')`,
		`INSERT INTO workspace_audit_events (id, workspace_id, actor_user_id, action, target_type, target_id, metadata, created_at)
		 VALUES ('e3', 'w1', NULL, 'server.added', 'server', 'web-1', '{}', '2026-01-02 03:04:07+00:00')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := goose.UpTo(database, ".", 14); err != nil {
		t.Fatal(err)
	}
	want := map[string][4]string{
		"e1": {"api", "ada@example.com", "blog", "web-1"},
		"e2": {"mcp", "ada@example.com", "blog", "web-1"},
		"e3": {"", "", "", "web-1"},
	}
	for id, columns := range want {
		var got [4]string
		err := database.QueryRow(`SELECT via, actor_email, app, server FROM workspace_audit_events WHERE id = ?`, id).Scan(&got[0], &got[1], &got[2], &got[3])
		if err != nil {
			t.Fatal(err)
		}
		if got != columns {
			t.Errorf("%s = %v, want %v", id, got, columns)
		}
	}
}
