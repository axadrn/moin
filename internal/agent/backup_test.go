package agent

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
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
	os.WriteFile(filepath.Join(a.appDir(app), "app-id"), []byte("app-one"), 0o600)

	_, err = b.snapshot(ctx, protocol.BackupRequest{App: app})
	if err == nil {
		t.Fatal("a snapshot without storage worked")
	}
	storage := &protocol.BackupStorage{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, Path: "test", Workspace: "ws-one", AccessKey: "x", SecretKey: "y"}
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

	// Two snapshots in one second keep both, the second numbered.
	first, err := b.snapshotOne(ctx, app, a.runningDatabases(app)[0], "burst")
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.snapshotOne(ctx, app, a.runningDatabases(app)[0], "burst")
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatedAt.Truncate(time.Second).Equal(second.CreatedAt.Truncate(time.Second)) && (second.ID == first.ID || second.Reason != "burst-2") {
		t.Fatalf("snapshots in one second: %s, %s", first.ID, second.ID)
	}

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
	list, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String("test/ws-one/" + app + "/app-one/data-app.db/")})
	if len(list.Contents) < 2 {
		t.Fatalf("bucket has %d objects for the app", len(list.Contents))
	}

	// The app removed and created again with the same name: the same file
	// replicates into a new, empty folder, and the old backups stay apart.
	os.WriteFile(filepath.Join(a.appDir(app), "app-id"), []byte("app-two"), 0o600)
	b.reconcile()
	snapshots, err := b.snapshots(ctx, app, "/data/app.db")
	if err != nil || len(snapshots) != 0 {
		t.Fatalf("the new app sees snapshots %v, %v", snapshots, err)
	}
	_, err = db.Exec(`INSERT INTO notes (body) VALUES ('new app')`)
	if err != nil {
		t.Fatal(err)
	}
	replicated(time.Now().Add(-time.Second))
	list, _ = client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String("test/ws-one/" + app + "/app-two/data-app.db/litestream/")})
	if len(list.Contents) == 0 {
		t.Fatal("nothing replicated under the new app ID")
	}
	old, _ := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String("test/ws-one/" + app + "/app-one/data-app.db/snapshots/")})
	if len(old.Contents) < 3 {
		t.Fatalf("the old app's snapshots are gone: %d left", len(old.Contents))
	}

	// Without an app ID, from a deploy before deploys brought one, nothing
	// is backed up and the status says what to do.
	os.Remove(filepath.Join(a.appDir(app), "app-id"))
	b.reconcile()
	if len(b.watched) != 0 {
		t.Fatalf("watched without an app ID = %v", b.watched)
	}
	if status := b.status(ctx, app, a.runningDatabases(app)[0], true); !strings.Contains(status.Error, "deploy "+app+" once more") {
		t.Fatalf("status without an app ID = %+v", status)
	}

	// The control plane sends the ID on connect: backups go on, no deploy.
	if !a.keepIDs(map[string]string{app: "app-two"}) {
		t.Fatal("the sent app ID was not kept")
	}
	b.reconcile()
	if len(b.watched) != 1 {
		t.Fatalf("watched with the sent app ID = %v", b.watched)
	}
}

// TestKeepIDs: an app without an ID takes the one the control plane sends;
// a stored ID, an unknown app or a bad ID are left alone.
func TestKeepIDs(t *testing.T) {
	a := newApps(t.TempDir(), nil)
	for _, app := range []string{"old", "new"} {
		os.MkdirAll(a.appDir(app), 0o700)
	}
	os.WriteFile(filepath.Join(a.appDir("new"), "app-id"), []byte("id-new"), 0o600)
	if !a.keepIDs(map[string]string{"old": "id-old", "new": "id-other", "gone": "id-gone", "../x": "id-x"}) {
		t.Fatal("nothing kept")
	}
	if a.keepIDs(map[string]string{"old": "id-again"}) {
		t.Fatal("kept over a stored ID")
	}
	if a.appID("old") != "id-old" || a.appID("new") != "id-new" || a.appID("gone") != "" {
		t.Fatalf("ids = %q %q %q", a.appID("old"), a.appID("new"), a.appID("gone"))
	}
	if _, err := os.Stat(a.appDir("gone")); err == nil {
		t.Fatal("an unknown app got a folder")
	}
	if a.keepIDs(map[string]string{"old": "../bad"}) {
		t.Fatal("kept a bad ID")
	}
}

// TestBucketLayout: two workspaces share one bucket and folder, each with
// an app of the same name. Their keys never overlap.
func TestBucketLayout(t *testing.T) {
	prefixes := map[string]string{}
	for _, workspace := range []string{"0b6c1f8e-1111-4c1a-9a7e-5d2b3c4d5e6f", "7a9e2d4c-2222-4b8f-8c3d-1e2f3a4b5c6d"} {
		for _, folder := range []string{"", "ops"} {
			a := newApps(t.TempDir(), nil)
			os.MkdirAll(a.appDir("shop"), 0o700)
			os.WriteFile(filepath.Join(a.appDir("shop"), "app-id"), []byte("id-"+workspace[:8]), 0o600)
			b := newBackups(a, nil)
			b.storage = &protocol.BackupStorage{Bucket: "shared", Path: folder, Workspace: workspace}
			prefix, err := b.prefix("shop", "/data/app.db")
			if err != nil {
				t.Fatal(err)
			}
			want := strings.TrimLeft(folder+"/"+workspace+"/shop/id-"+workspace[:8]+"/data-app.db", "/")
			if prefix != want {
				t.Errorf("prefix = %s, want %s", prefix, want)
			}
			for other, key := range prefixes {
				if strings.HasPrefix(prefix+"/", key+"/") || strings.HasPrefix(key+"/", prefix+"/") {
					t.Errorf("%s overlaps %s (%s)", prefix, key, other)
				}
			}
			prefixes[workspace+folder] = prefix
		}
	}
	err := newBackups(newApps(t.TempDir(), nil), nil).configure(context.Background(), &protocol.BackupStorage{Bucket: "shared"})
	if err == nil {
		t.Error("a bucket without a workspace folder was accepted")
	}
}

// TestSweepUploads: uploads older than a day go, newer ones and backups
// stay, and another workspace's uploads are never touched.
func TestSweepUploads(t *testing.T) {
	endpoint := os.Getenv("OPS_TEST_S3")
	if endpoint == "" {
		t.Skip("set OPS_TEST_S3 to an S3 endpoint")
	}
	bucket := os.Getenv("OPS_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "ops-dev"
	}
	ctx := context.Background()
	folder := fmt.Sprintf("sweep-%d", time.Now().UnixNano())
	b := newBackups(newApps(t.TempDir(), nil), nil)
	b.storage = &protocol.BackupStorage{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, Path: folder, Workspace: "ws-one", AccessKey: "x", SecretKey: "y"}
	client := b.s3Client()
	keys := []string{
		folder + "/ws-one/shop/app-1/uploads/up_old.tar.gz",
		folder + "/ws-one/gone/app-2/uploads/up_old.tar.gz",
		folder + "/ws-one/shop/app-1/data-app.db/snapshots/20261031T143638Z-manual.db.gz",
		folder + "/ws-two/shop/app-3/uploads/up_old.tar.gz",
	}
	for _, key := range keys {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader("x")})
		if err != nil {
			t.Fatal(err)
		}
	}
	exists := func(key string) bool {
		_, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		return err == nil
	}
	b.sweepOnce(ctx, time.Now().Add(-time.Hour))
	for _, key := range keys {
		if !exists(key) {
			t.Fatalf("%s went before it was a day old", key)
		}
	}
	b.sweepOnce(ctx, time.Now().Add(time.Minute))
	want := map[string]bool{keys[0]: false, keys[1]: false, keys[2]: true, keys[3]: true}
	for key, kept := range want {
		if exists(key) != kept {
			t.Errorf("%s exists = %v, want %v", key, !kept, kept)
		}
		client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	}
}

// TestDrillBeforeReplication: a drill with nothing in the bucket yet is
// neither passed nor failed, and asks to try again soon.
func TestDrillBeforeReplication(t *testing.T) {
	endpoint := os.Getenv("OPS_TEST_S3")
	if endpoint == "" {
		t.Skip("set OPS_TEST_S3 to an S3 endpoint")
	}
	bucket := os.Getenv("OPS_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "ops-dev"
	}
	reports := make(chan protocol.Backups, 1)
	apps := newApps(t.TempDir(), func(protocol.MessageType, string, any) {})
	os.MkdirAll(apps.appDir("fresh"), 0o700)
	os.WriteFile(filepath.Join(apps.appDir("fresh"), "app-id"), []byte("app-1"), 0o600)
	b := newBackups(apps, func(_ protocol.MessageType, _ string, data any) {
		if backups, ok := data.(protocol.Backups); ok {
			reports <- backups
		}
	})
	b.storage = &protocol.BackupStorage{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, Path: fmt.Sprintf("empty-%d", time.Now().UnixNano()), Workspace: "ws-one", AccessKey: "x", SecretKey: "y"}
	database := protocol.Database{Path: "/data/app.db", HostPath: filepath.Join(t.TempDir(), "app.db")}
	b.watched[database.HostPath] = watched{app: "fresh", prefix: path.Join(b.storage.Path, "ws-one/fresh/app-1/data-app.db"), database: database}
	if !b.drillAll(context.Background()) {
		t.Error("a drill before the first replication does not try again soon")
	}
	report := <-reports
	if report.Databases[0].VerifiedAt != nil || report.Databases[0].Error != "" {
		t.Errorf("report = %+v, want no drill yet", report.Databases[0])
	}
}

// TestDeployRefusesBadAppID: the app ID becomes a folder in the bucket, so
// a deploy with one that could leave it fails before anything happens.
func TestDeployRefusesBadAppID(t *testing.T) {
	a := newApps(t.TempDir(), func(protocol.MessageType, string, any) {})
	_, err := a.runDeploy(context.Background(), protocol.DeployStart{App: "shop", AppID: "../other", Number: 1}, func(string, ...any) {})
	if err == nil {
		t.Fatal("a deploy with a bad app ID ran")
	}
	if a.appID("shop") != "" {
		t.Error("a bad app ID was stored")
	}
}
