//go:build docker

package daemon

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// The promises of backups against a real Redis and a real Valkey: a copy,
// a restore, its undo, a restore that was cut off, the daily check, and a
// broken copy that must not pass it. One keeps its data in dumps, one in
// an append only file behind a password. Run like the other backup tests.
func TestBackupRedis(t *testing.T) {
	for image, command := range map[string][]string{
		"redis:7":         {"redis-server", "--appendonly", "yes", "--requirepass", "s3'cret\" x"},
		"redis:8":         {"redis-server", "--save", "60", "1"},
		"valkey/valkey:9": {"valkey-server", "--appendonly", "yes", "--port", "6380"},
	} {
		t.Run(image, func(t *testing.T) { testBackupRedis(t, image, command) })
	}
}

func testBackupRedis(t *testing.T, image string, command []string) {
	app := fmt.Sprintf("bk-redis-%d", time.Now().UnixNano()%1e6)
	compose := map[string]any{
		"services": map[string]any{
			"web":   map[string]any{"image": "alpine:latest", "command": []string{"sleep", "infinity"}, "init": true},
			"queue": map[string]any{"image": image, "command": command, "volumes": []string{"queue:/data"}},
		},
		"volumes": map[string]any{"queue": map[string]any{}},
	}
	database := protocol.Database{Path: protocol.ServiceKey(protocol.DatabaseRedis, "queue"), Kind: protocol.DatabaseRedis, Service: "queue"}
	test := newBackupTest(t, app, compose, database)
	engine := newRedisEngine(test.b, app, database)
	target, err := engine.target(test.ctx)
	if err == nil {
		err = waitFor(test.ctx, target.answers)
	}
	if err != nil {
		t.Fatal(err)
	}
	ask := func(args ...string) string {
		t.Helper()
		out, err := target.ask(test.ctx, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	rows := func() string { return strings.Join(strings.Fields(ask("LRANGE", "notes", "0", "-1")), ",") }
	ask("RPUSH", "notes", "a", "b")
	ask("HSET", "user:1", "name", "Ada", "picture", "\xff'\"\\\n")
	if size, err := engine.Size(test.ctx); err != nil || size == 0 {
		t.Fatalf("the size: %d %v", size, err)
	}

	// The first copy of the day is checked before it is uploaded.
	test.b.tick(test.ctx)
	first := test.copies()[0]
	test.b.saveStates()
	if status := newBackups(test.a).status(test.ctx, app, database, false); status.CheckError != "" || status.CheckedCopy != first.ID {
		t.Fatalf("the first copy was not checked before its upload: %+v", status)
	}
	ask("RPUSH", "notes", "c")
	ask("SET", "later", "1")
	second := test.copyNow()
	test.encryptedForBoth(second)

	// The copy in the bucket is a gzipped dump of Redis.
	from := mustCopy(t, test, second.ID)
	if from.size == 0 {
		t.Fatal("the copy carries no note of its size")
	}
	unzipped, err := gzip.NewReader(from.copy)
	if err != nil {
		t.Fatal(err)
	}
	start := make([]byte, 5)
	if _, err := unzipped.Read(start); err != nil || (string(start) != "REDIS" && string(start) != "VALKE") {
		t.Fatalf("the copy is no dump of Redis: %q %v", start, err)
	}

	if state := test.restore(protocol.BackupRestore{Copy: first.ID}); state.State != protocol.RestoreDone {
		t.Fatalf("restore to the first copy: %+v", state)
	}
	if got := rows(); got != "a,b" || !test.running() {
		t.Fatalf("after the restore: %q, running %v", got, test.running())
	}
	// A key made after the copy is gone, the Redis is its own again, and
	// what it took is on its disk: it is still there after a restart.
	if got := ask("EXISTS", "later") + " " + ask("HGET", "user:1", "name"); got != "0 Ada" {
		t.Fatalf("after the restore: %q", got)
	}
	if info := ask("INFO", "replication"); !strings.Contains(info, "role:master") {
		t.Fatalf("after the restore the Redis follows another: %s", info)
	}
	if out, err := exec.Command("docker", "restart", target.container).CombinedOutput(); err != nil {
		t.Fatalf("restart: %v %s", err, out)
	}
	if err := waitFor(test.ctx, target.answers); err != nil {
		t.Fatal(err)
	}
	if got := rows(); got != "a,b" {
		t.Fatalf("after the restore and a restart: %q", got)
	}
	undos, _ := engine.Undos(test.ctx)
	if len(undos) != 1 || undos[0].Size == 0 {
		t.Fatalf("undos %+v", undos)
	}
	if state := test.restore(protocol.BackupRestore{Undo: undos[0].ID}); state.State != protocol.RestoreDone {
		t.Fatalf("undo: %+v", state)
	}
	if got := rows(); got != "a,b,c" {
		t.Fatalf("after the undo: %q", got)
	}
	// The undo it went back to is used up; the restore left a new one.
	if undos, _ := engine.Undos(test.ctx); len(undos) != 1 {
		t.Fatalf("undos after the undo: %+v", undos)
	}

	// The daily check passes a good copy. The check and the restore
	// leave no container.
	if why := test.check(); why != "" {
		t.Fatalf("the check failed: %s", why)
	}
	for _, what := range []string{"check-", "restore-"} {
		if out, _ := exec.Command("docker", "ps", "-a", "-q", "--filter", "name="+tempName(what)).Output(); len(bytes.TrimSpace(out)) != 0 {
			t.Fatalf("a %s container is left: %s", what, out)
		}
	}

	// A copy that does not load changes nothing: the undo goes back in.
	test.putBroken()
	broken := test.copies()[0]
	if state := test.restore(protocol.BackupRestore{Copy: broken.ID}); state.State != protocol.RestoreFailed || !strings.Contains(state.Error, "as it was before") {
		t.Fatalf("restore to a broken copy: %+v", state)
	}
	if got := rows(); got != "a,b,c" || !test.running() {
		t.Fatalf("after the restore to a broken copy: %q, running %v", got, test.running())
	}

	// A restore that was cut off, before anything changed and halfway
	// through the load: the data is as it was before, and the app runs.
	for _, step := range []string{"before", "halfway"} {
		if _, err := test.b.apps.stopServices(test.ctx, app, test.b.otherServices(app, "queue")...); err != nil {
			t.Fatal(err)
		}
		mark := test.killed()
		if step == "halfway" {
			if err := engine.keep(test.ctx, mark.Undo); err != nil {
				t.Fatal(err)
			}
			ask("FLUSHALL")
		}
		test.b.recover(test.ctx)
		state := test.stateOf(mark.ID)
		if state.State != protocol.RestoreFailed || !strings.Contains(state.Error, "as it was before") {
			t.Fatalf("cut off %s: %+v", step, state)
		}
		if got := rows(); got != "a,b,c" || !test.running() {
			t.Fatalf("cut off %s: %q, running %v", step, got, test.running())
		}
	}
	if undos, _ := engine.Undos(test.ctx); len(undos) != 1 {
		t.Fatalf("a restore that was cut off left its undo: %+v", undos)
	}

	// The broken copy is the newest now: it must not pass the check.
	if why := test.check(); !strings.Contains(why, "did not restore") {
		t.Fatalf("a broken copy passed the check: %q", why)
	}
	if listed, err := test.listed(); err != nil || len(listed) != 1 || listed[0].Database != "redis-queue" || listed[0].Kind != "redis" {
		t.Fatalf("listed %+v %v", listed, err)
	}
}
