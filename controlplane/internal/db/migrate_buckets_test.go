package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestBucketsMigrationKeepsTheBucket: the one bucket of a workspace before
// 023 becomes its default bucket, named default, with the same sealed
// secret key, and its apps follow the default. A workspace without a
// bucket gets none. Down puts the single bucket back.
func TestBucketsMigrationKeepsTheBucket(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "buckets.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := setupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database, ".", 19); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO workspaces (id, name, slug) VALUES ('w1', 'Acme', 'acme')`,
		`INSERT INTO workspaces (id, name, slug) VALUES ('w2', 'Other', 'other')`,
		`INSERT INTO servers (id, workspace_id, name) VALUES ('s1', 'w1', 'web')`,
		`INSERT INTO apps (id, workspace_id, server_id, name) VALUES ('a1', 'w1', 's1', 'shop')`,
		`INSERT INTO backup_storage (workspace_id, endpoint, region, bucket, path, access_key, secret_key, created_at)
			VALUES ('w1', 'https://r2.example.com', 'auto', 'acme-backups', 'moin', 'AKIA1', 'sealed-secret', '2026-10-01 10:00:00')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := goose.UpTo(database, ".", 23); err != nil {
		t.Fatal(err)
	}

	var id, workspace, name, endpoint, region, bucket, path, accessKey, secretKey string
	var isDefault bool
	err = database.QueryRow(`SELECT id, workspace_id, name, is_default, endpoint, region, bucket, path, access_key, secret_key FROM buckets`).
		Scan(&id, &workspace, &name, &isDefault, &endpoint, &region, &bucket, &path, &accessKey, &secretKey)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || workspace != "w1" || name != "default" || !isDefault || endpoint != "https://r2.example.com" || region != "auto" ||
		bucket != "acme-backups" || path != "moin" || accessKey != "AKIA1" || secretKey != "sealed-secret" {
		t.Fatalf("bucket = %s %s %s %v %s %s %s %s %s %s", id, workspace, name, isDefault, endpoint, region, bucket, path, accessKey, secretKey)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM buckets`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("buckets = %d, %v; the workspace without a bucket got one", count, err)
	}
	var bucketID sql.NullString
	if err := database.QueryRow(`SELECT bucket_id FROM apps WHERE id = 'a1'`).Scan(&bucketID); err != nil || bucketID.Valid {
		t.Fatalf("shop's bucket = %v, %v; want the default", bucketID, err)
	}
	if _, err := database.Exec(`SELECT 1 FROM backup_storage`); err == nil {
		t.Fatal("backup_storage is still there")
	}
	// Two defaults in one workspace cannot be.
	_, err = database.Exec(`INSERT INTO buckets (id, workspace_id, name, is_default, endpoint, region, bucket, access_key, secret_key) VALUES ('b2', 'w1', 'second', 1, '', 'auto', 'b', 'k', 's')`)
	if err == nil {
		t.Fatal("a second default bucket was stored")
	}

	if err := goose.DownTo(database, ".", 19); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT bucket, secret_key FROM backup_storage WHERE workspace_id = 'w1'`).Scan(&bucket, &secretKey); err != nil || bucket != "acme-backups" || secretKey != "sealed-secret" {
		t.Fatalf("after down: %s %s %v", bucket, secretKey, err)
	}
}
