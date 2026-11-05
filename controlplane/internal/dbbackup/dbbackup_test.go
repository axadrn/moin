package dbbackup

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestFile(t *testing.T) {
	for connection, want := range map[string]string{
		"./data/app.db?_pragma=foreign_keys(1)": "./data/app.db",
		"file:/var/lib/app.db?mode=rwc":         "/var/lib/app.db",
		":memory:":                              "",
		"postgres://user@host/db":               "",
	} {
		if got := File(connection); got != want {
			t.Errorf("File(%q) = %q, want %q", connection, got, want)
		}
	}
}

// TestBackupAndRestore needs an S3 endpoint with the bucket in
// MOIN_TEST_S3_BUCKET, such as gofakes3: MOIN_TEST_S3=http://127.0.0.1:19000.
func TestBackupAndRestore(t *testing.T) {
	endpoint := os.Getenv("MOIN_TEST_S3")
	if endpoint == "" {
		t.Skip("set MOIN_TEST_S3 to an S3 endpoint")
	}
	name := os.Getenv("MOIN_TEST_S3_BUCKET")
	if name == "" {
		name = "moin-dev"
	}
	bucket := Bucket{Endpoint: endpoint, Region: "us-east-1", Bucket: name, AccessKey: "x", SecretKey: "y", Folder: fmt.Sprintf("test-%d/litestream", time.Now().UnixNano())}
	ctx := context.Background()

	// A fresh start with an empty bucket.
	file := filepath.Join(t.TempDir(), "app.db")
	if err := RestoreIfMissing(ctx, bucket, file); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+file+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE users (email TEXT); INSERT INTO users VALUES ('ada@example.com')`); err != nil {
		t.Fatal(err)
	}
	stop, err := Start(ctx, bucket, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users VALUES ('grace@example.com')`); err != nil {
		t.Fatal(err)
	}

	// A new server: no file, and the database comes back from the bucket.
	restored := filepath.Join(t.TempDir(), "app.db")
	var count int
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		os.Remove(restored)
		if err := RestoreIfMissing(ctx, bucket, restored); err == nil {
			check, _ := sql.Open("sqlite", "file:"+restored)
			check.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
			check.Close()
			if count == 2 {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	stop()
	db.Close()
	if count != 2 {
		t.Fatalf("restored %d users, want 2", count)
	}
}
