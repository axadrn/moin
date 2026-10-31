package agent

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/ops/protocol"
)

// TestBackups needs an S3 endpoint with the bucket in OPS_TEST_S3_BUCKET,
// such as gofakes3: OPS_TEST_S3=http://127.0.0.1:19000.
func TestBackups(t *testing.T) {
	endpoint := os.Getenv("OPS_TEST_S3")
	if endpoint == "" {
		t.Skip("set OPS_TEST_S3 to an S3 endpoint")
	}
	bucket := os.Getenv("OPS_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "ops-dev"
	}
	ctx := context.Background()
	dir := t.TempDir()
	a := newApps(dir, func(protocol.MessageType, string, any) {})
	reports := make(chan protocol.Backups, 10)
	b := newBackups(a, func(messageType protocol.MessageType, _ string, data any) {
		if backups, ok := data.(protocol.Backups); ok && messageType == protocol.TypeBackupStatus {
			reports <- backups
		}
	})
	a.backups = b

	file := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", "file:"+file+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT); INSERT INTO notes (body) VALUES ('one'), ('two')`)
	if err != nil {
		t.Fatal(err)
	}
	app := fmt.Sprintf("backup-%d", time.Now().UnixNano())
	release := a.releaseDir(app, 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{{Path: "/data/app.db", HostPath: file}}})
	os.WriteFile(filepath.Join(a.appDir(app), "current"), []byte("1"), 0o600)

	_, err = b.snapshot(ctx, protocol.BackupRequest{App: app})
	if err == nil {
		t.Fatal("a snapshot without storage worked")
	}
	storage := &protocol.BackupStorage{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, Path: "test", AccessKey: "x", SecretKey: "y"}
	err = b.configure(ctx, storage)
	if err != nil {
		t.Fatal(err)
	}
	defer b.configure(ctx, nil)
	if len(b.watched) != 1 {
		t.Fatalf("watched = %v", b.watched)
	}

	backups, err := b.snapshot(ctx, protocol.BackupRequest{App: app, Reason: "Before v2!"})
	if err != nil {
		t.Fatal(err)
	}
	if len(backups.Databases) != 1 || len(backups.Databases[0].Snapshots) != 1 || backups.Databases[0].Snapshots[0].Reason != "before-v2" {
		t.Fatalf("backups = %+v", backups)
	}
	snapshot := backups.Databases[0].Snapshots[0]

	_, err = db.Exec(`INSERT INTO notes (body) VALUES ('three')`)
	if err != nil {
		t.Fatal(err)
	}
	replicated := func(after time.Time) protocol.DatabaseBackup {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			status := b.status(ctx, app, a.runningDatabases(app)[0], false)
			if status.ReplicatedAt != nil && status.ReplicatedAt.After(after) {
				return status
			}
			time.Sleep(300 * time.Millisecond)
		}
		t.Fatalf("nothing replicated after %s", after)
		return protocol.DatabaseBackup{}
	}
	replicated(time.Now().Add(-time.Second))
	time.Sleep(1100 * time.Millisecond)
	beforeDelete := time.Now()
	time.Sleep(1100 * time.Millisecond)
	_, err = db.Exec(`DELETE FROM notes`)
	if err != nil {
		t.Fatal(err)
	}
	status := replicated(beforeDelete)
	if status.Since == nil {
		t.Fatalf("no point in time window: %+v", status)
	}

	count := func(path string) int {
		check, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer check.Close()
		var n int
		err = check.QueryRow(`SELECT count(*) FROM notes`).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	pointFile := filepath.Join(dir, "point.db")
	err = b.restorePoint(ctx, app, "/data/app.db", beforeDelete, pointFile)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(pointFile); n != 3 {
		t.Fatalf("point in time before the delete has %d notes, want 3", n)
	}
	snapshotFile := filepath.Join(dir, "snapshot.db")
	err = b.download(ctx, app, "/data/app.db", snapshot.ID, snapshotFile)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(snapshotFile); n != 2 {
		t.Fatalf("snapshot has %d notes, want 2", n)
	}

	b.drillAll(ctx)
	select {
	case report := <-reports:
		if report.App != app || report.Databases[0].VerifiedAt == nil || report.Databases[0].Error != "" {
			t.Fatalf("drill report = %+v", report)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no drill report")
	}

	// A new folder in the bucket starts over and replicates again.
	moved := *storage
	moved.Path = "test-moved"
	err = b.configure(ctx, &moved)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO notes (body) VALUES ('after the move')`)
	if err != nil {
		t.Fatal(err)
	}
	replicated(time.Now().Add(-time.Second))
	b.drillAll(ctx)
	select {
	case report := <-reports:
		if report.Databases[0].Error != "" {
			t.Fatalf("drill after the move = %+v", report)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no drill report after the move")
	}
	err = b.configure(ctx, storage)
	if err != nil {
		t.Fatal(err)
	}

	client := b.s3Client()
	list, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String("test/" + app + "/data-app.db/")})
	if len(list.Contents) < 2 {
		t.Fatalf("bucket has %d objects for the app", len(list.Contents))
	}
}
