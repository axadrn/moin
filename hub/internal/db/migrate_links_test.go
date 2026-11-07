package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestLinksMigration: a bucket 023 made with an ID without dashes gets
// them, and so do the apps that point at it; an app's folder is its name.
func TestLinksMigration(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "links.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := setupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, ".", 38); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO workspaces (id, name, slug) VALUES ('w1', 'Acme', 'acme')`,
		`INSERT INTO servers (id, workspace_id, name) VALUES ('s1', 'w1', 'web-1')`,
		`INSERT INTO buckets (id, workspace_id, name, is_default, endpoint, region, bucket, access_key, secret_key)
			VALUES ('0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b', 'w1', 'default', 1, '', 'auto', 'b', 'k', 's')`,
		`INSERT INTO buckets (id, workspace_id, name, endpoint, region, bucket, access_key, secret_key)
			VALUES ('11111111-2222-3333-4444-555555555555', 'w1', 'second', '', 'auto', 'b', 'k', 's')`,
		`INSERT INTO apps (id, workspace_id, server_id, name, bucket_id) VALUES ('a1', 'w1', 's1', 'my-app', '0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b')`,
		`INSERT INTO app_buckets (app_id, bucket_id) VALUES ('a1', '0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := goose.UpTo(database, ".", 39); err != nil {
		t.Fatal(err)
	}
	const dashed = "0f8e2c4a-1b3d-4e5f-8a9b-0c1d2e3f4a5b"
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM buckets WHERE id IN ($1, '11111111-2222-3333-4444-555555555555')`, dashed).Scan(&count); err != nil || count != 2 {
		t.Fatalf("buckets with dashed IDs = %d, %v", count, err)
	}
	var bucketID, left, folder string
	if err := database.QueryRow(`SELECT a.bucket_id, b.bucket_id, a.folder FROM apps a JOIN app_buckets b ON b.app_id = a.id`).Scan(&bucketID, &left, &folder); err != nil {
		t.Fatal(err)
	}
	if bucketID != dashed || left != dashed || folder != "my-app" {
		t.Fatalf("app bucket %s, left %s, folder %s", bucketID, left, folder)
	}
	if err := goose.DownTo(database, ".", 38); err != nil {
		t.Fatal(err)
	}
}
