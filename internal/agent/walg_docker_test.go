//go:build docker

package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/axadrn/ops/protocol"
)

// TestWALGPointInTime runs a real postgres:17 as the service db of the app
// walgtest, with a local S3 (SeaweedFS) next to it. It writes rows at T1, more at
// T2, restores to a time between them and checks that exactly the T1 rows
// are back. Run with: go test -tags docker -run WALG ./internal/agent/
// OPS_WALG_BINARY skips the download of WAL-G.
func TestWALGPointInTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	walgBinaryPath = os.Getenv("OPS_WALG_BINARY")
	defer func() { walgBinaryPath = "" }()

	root := t.TempDir()
	a := newApps(root, nil)
	b := newBackups(a, func(protocol.MessageType, string, any) {})
	a.backups = b
	const app = "walgtest"
	release := a.releaseDir(app, 1)
	if err := os.MkdirAll(release, 0o700); err != nil {
		t.Fatal(err)
	}
	compose := map[string]any{
		"services": map[string]any{
			"db": map[string]any{
				"image":       "postgres:17",
				"environment": map[string]string{"POSTGRES_USER": "app", "POSTGRES_DB": "shop", "POSTGRES_PASSWORD": "secret"},
				"volumes":     []any{map[string]any{"type": "volume", "source": "pgdata", "target": "/var/lib/postgresql/data"}},
			},
		},
		"volumes": map[string]any{"pgdata": map[string]any{"name": "ops-walgtest-pgdata"}},
	}
	composeFile := filepath.Join(release, "compose.json")
	saveJSON(composeFile, compose)
	database := protocol.Database{Path: protocol.PostgresKey("db"), Kind: protocol.DatabasePostgres, Service: "db"}
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{database}})
	os.WriteFile(filepath.Join(a.appDir(app), "current"), []byte("1"), 0o600)
	os.WriteFile(filepath.Join(a.appDir(app), "app-id"), []byte("4f6c1d2e-0000-4000-8000-000000000001"), 0o600)
	down := func() {
		exec.Command("docker", "compose", "-p", project(app), "-f", composeFile, "down", "-v").Run()
		exec.Command("docker", "rm", "-f", companionName(app, "db"), "ops-walgtest-s3").Run()
	}
	down()
	defer down()
	// A local S3 for this test only, outside the app: a restore stops and
	// starts the app's services, never the bucket.
	if out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--name", "ops-walgtest-s3", "-p", "19000:8333", "chrislusf/seaweedfs:latest", "server", "-s3", "-s3.port=8333", "-dir=/data").CombinedOutput(); err != nil {
		t.Fatalf("s3: %v %s", err, out)
	}
	if out, err := exec.CommandContext(ctx, "docker", "compose", "-p", project(app), "-f", composeFile, "up", "-d").CombinedOutput(); err != nil {
		t.Fatalf("compose up: %v %s", err, out)
	}
	gateway, err := exec.Command("docker", "network", "inspect", "-f", "{{(index .IPAM.Config 0).Gateway}}", project(app)+"_default").Output()
	if err != nil {
		t.Fatal(err)
	}
	storage := &protocol.BackupStorage{Endpoint: "http://" + strings.TrimSpace(string(gateway)) + ":19000", Region: "us-east-1", Bucket: "ops-test", Workspace: "ws1", AccessKey: "opstest", SecretKey: "opstestsecret", Name: "test"}
	client := s3Client(storage)
	for deadline := time.Now().Add(time.Minute); ; {
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(storage.Bucket)})
		if err == nil || strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bucket: %v", err)
		}
		time.Sleep(time.Second)
	}
	b.storage = storage

	target, err := a.postgres(ctx, app, database)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Minute); ; {
		if exec.Command("docker", "exec", target.container, "pg_isready", "-q", "-h", "127.0.0.1", "-U", "app").Run() == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("postgres did not start")
		}
		time.Sleep(time.Second)
	}

	// The companion starts and takes the first base backup.
	b.walgOnce(ctx)
	running, _ := a.output(ctx, "docker", "inspect", "-f", "{{.State.Running}}", companionName(app, "db"))
	if strings.TrimSpace(running) != "true" {
		logs, _ := exec.Command("docker", "logs", companionName(app, "db")).CombinedOutput()
		t.Fatalf("companion not running: %s", logs)
	}
	bases, err := b.walgBaseBackups(ctx, app, database)
	if err != nil || len(bases) != 1 {
		t.Fatalf("base backups %v %v", bases, err)
	}

	sql := func(script string) string {
		out, err := target.psql(ctx, script, nil, false)
		if err != nil {
			t.Fatalf("%s: %v", script, err)
		}
		return strings.TrimSpace(out)
	}
	// waitWAL closes the segment and waits until the bucket has more WAL.
	waitWAL := func() {
		before, _ := b.walgObjects(ctx, app, database, "wal_005")
		if err := b.walgSwitch(ctx, app, database); err != nil {
			t.Fatal(err)
		}
		for deadline := time.Now().Add(2 * time.Minute); ; {
			now, _ := b.walgObjects(ctx, app, database, "wal_005")
			if now > before {
				return
			}
			if time.Now().After(deadline) {
				logs, _ := exec.Command("docker", "logs", "--tail", "20", companionName(app, "db")).CombinedOutput()
				t.Fatalf("no new WAL in the bucket after a switch: %s", logs)
			}
			time.Sleep(time.Second)
		}
	}
	sql(`CREATE TABLE orders (id int PRIMARY KEY, at text); INSERT INTO orders VALUES (1, 'T1'), (2, 'T1');`)
	waitWAL()
	time.Sleep(2 * time.Second)
	between := time.Now().UTC()
	time.Sleep(2 * time.Second)
	sql(`INSERT INTO orders VALUES (3, 'T2'), (4, 'T2'); UPDATE orders SET at = 'T2' WHERE id = 1;`)
	waitWAL()

	status := b.status(ctx, app, database, true)
	if status.Since == nil || status.ReplicatedAt == nil || status.ReplicatedAt.Before(between) || status.Note != "" {
		t.Fatalf("status %+v", status)
	}

	err = b.restore(ctx, protocol.BackupRestore{App: app, Database: database.Path, At: &between})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	target, err = a.postgres(ctx, app, database)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Minute); ; {
		if exec.Command("docker", "exec", target.container, "pg_isready", "-q", "-h", "127.0.0.1", "-U", "app").Run() == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("postgres did not come back")
		}
		time.Sleep(time.Second)
	}
	got := sql(`SELECT string_agg(id || ':' || at, ',' ORDER BY id) FROM orders;`)
	if got != "1:T1,2:T1" {
		t.Fatalf("after the restore to %s: %s, want 1:T1,2:T1", between, got)
	}
	// The snapshot before the restore holds the T2 state.
	snapshots, _ := b.snapshots(ctx, app, database.Path)
	if len(snapshots) == 0 || snapshots[0].Reason != "before-restore" {
		t.Errorf("snapshots %+v", snapshots)
	}
	// A time outside the window is refused, and nothing changes.
	future := time.Now().Add(time.Hour)
	if err := b.restore(ctx, protocol.BackupRestore{App: app, Database: database.Path, At: &future}); err == nil || !strings.Contains(err.Error(), "outside the point in time window") {
		t.Errorf("restore to the future: %v", err)
	}

	// The slot is capped, so a WAL-G that stays down cannot fill the disk.
	b.walgOnce(ctx)
	if got := sql(`SHOW max_slot_wal_keep_size;`); got != "4GB" {
		t.Errorf("max_slot_wal_keep_size = %s, want 4GB", got)
	}
	// WAL-G stays down past a (here tiny) cap: Postgres drops its place,
	// ops notices, starts a new companion and takes a new base backup.
	for deadline := time.Now().Add(time.Minute); sql(`SELECT count(*) FROM pg_replication_slots WHERE slot_name = 'ops_walg' AND restart_lsn IS NOT NULL;`) != "1"; {
		if time.Now().After(deadline) {
			t.Fatal("WAL-G never started streaming")
		}
		time.Sleep(time.Second)
	}
	if out, err := exec.Command("docker", "stop", companionName(app, "db")).CombinedOutput(); err != nil {
		t.Fatalf("stop the companion: %v %s", err, out)
	}
	sql(`ALTER SYSTEM SET max_slot_wal_keep_size = '1MB';`)
	sql(`SELECT pg_reload_conf();`)
	lost := false
	for round := 0; round < 20 && !lost; round++ {
		sql(fmt.Sprintf(`INSERT INTO orders SELECT g, 'x' FROM generate_series(%d, %d) g; SELECT pg_switch_wal();`, 1000+round*1000, 1999+round*1000))
		sql(`CHECKPOINT;`)
		lost = sql(`SELECT coalesce((SELECT wal_status FROM pg_replication_slots WHERE slot_name = 'ops_walg'), '');`) == "lost"
	}
	if !lost {
		t.Fatalf("the slot never got lost: %s; cap %s; lsn %s", sql(`SELECT string_agg(slot_name || ' ' || coalesce(wal_status, '-') || ' ' || active || ' ' || coalesce(restart_lsn::text, '-'), '; ') FROM pg_replication_slots;`), sql(`SHOW max_slot_wal_keep_size;`), sql(`SELECT pg_current_wal_lsn();`))
	}
	basesBefore, _ := b.walgBaseBackups(ctx, app, database)
	b.walgOnce(ctx)
	if got := sql(`SELECT coalesce((SELECT wal_status FROM pg_replication_slots WHERE slot_name = 'ops_walg'), 'none');`); got == "lost" {
		t.Errorf("the slot is still lost")
	}
	running, _ = a.output(ctx, "docker", "inspect", "-f", "{{.State.Running}}", companionName(app, "db"))
	if strings.TrimSpace(running) != "true" {
		t.Errorf("no companion after the lost slot")
	}
	basesAfter, _ := b.walgBaseBackups(ctx, app, database)
	if len(basesAfter) <= len(basesBefore) {
		t.Errorf("no new base backup: %d before, %d after", len(basesBefore), len(basesAfter))
	}
	status = b.status(ctx, app, database, false)
	if !strings.Contains(status.Note, "Continuous backup broke") || !strings.Contains(status.Note, "took a new base backup") {
		t.Errorf("note %q", status.Note)
	}
	if status.Since == nil || status.Since.Before(basesAfter[len(basesAfter)-1].at.Add(-time.Second)) {
		t.Errorf("the window starts before the new base backup: %v", status.Since)
	}

	// Without a bucket the companion goes and its slot with it.
	b.walgOnce(ctx)
	b.storage = nil
	b.walgOnce(ctx)
	if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "label=ops.walg").Output(); strings.TrimSpace(string(out)) != "" {
		t.Errorf("companion left: %s", out)
	}
	if slots := sql(`SELECT count(*) FROM pg_replication_slots;`); slots != "0" {
		t.Errorf("slots left: %s", slots)
	}
	out, _ := exec.Command("docker", "volume", "ls", "-q", "--filter", "name=ops-restore-").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("restore volumes left: %s", out)
	}
}
