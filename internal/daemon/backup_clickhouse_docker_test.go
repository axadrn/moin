//go:build docker

package daemon

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// The promises of backups against a real ClickHouse: a copy, a restore,
// its undo, a restore that was cut off, the daily check, and a broken
// copy that must not pass it. Once as Plausible runs it, without a user,
// and once with a user and a password. Run like the other backup tests.
func TestBackupClickhouse(t *testing.T) {
	for image, environment := range map[string]map[string]string{
		"clickhouse/clickhouse-server:24.12-alpine": {"CLICKHOUSE_SKIP_USER_SETUP": "1"},
		"clickhouse/clickhouse-server:25.8":         {"CLICKHOUSE_USER": "app", "CLICKHOUSE_PASSWORD": "s3'cret\" x", "CLICKHOUSE_DB": "shop"},
	} {
		t.Run(image, func(t *testing.T) { testBackupClickhouse(t, image, environment) })
	}
}

func testBackupClickhouse(t *testing.T, image string, environment map[string]string) {
	app := fmt.Sprintf("bk-ch-%d", time.Now().UnixNano()%1e6)
	compose := map[string]any{"services": map[string]any{
		"web":    map[string]any{"image": "alpine:latest", "command": []string{"sleep", "infinity"}, "init": true},
		"events": map[string]any{"image": image, "environment": environment},
	}}
	database := protocol.Database{Path: protocol.ServiceKey(protocol.DatabaseClickHouse, "events"), Kind: protocol.DatabaseClickHouse, Service: "events"}
	test := newBackupTest(t, app, compose, database)
	engine := newClickhouseEngine(test.b, app, database)
	target, err := engine.target(test.ctx)
	if err == nil {
		err = waitFor(test.ctx, target.answers)
	}
	if err != nil {
		t.Fatal(err)
	}
	sql := func(statements string) string {
		t.Helper()
		out, err := target.sql(test.ctx, statements)
		if err != nil {
			t.Fatalf("%s: %v", statements, err)
		}
		return out
	}
	rows := func() string {
		return sql("SELECT arrayStringConcat(groupArray(body), ',') FROM (SELECT body FROM shop.notes ORDER BY id);")
	}
	sql("CREATE DATABASE IF NOT EXISTS shop;" +
		"CREATE TABLE shop.notes (id UInt64, body String) ENGINE = MergeTree ORDER BY id;" +
		"CREATE TABLE shop.`odd ``name` (id UInt64) ENGINE = Log;" +
		"CREATE MATERIALIZED VIEW shop.counted ENGINE = SummingMergeTree ORDER BY body AS SELECT body, count() AS times FROM shop.notes GROUP BY body;" +
		"INSERT INTO shop.notes VALUES (1, 'a'), (2, 'b');" +
		"INSERT INTO shop.`odd ``name` VALUES (7);" +
		"CREATE TABLE default.visits (id UInt64) ENGINE = MergeTree ORDER BY id;" +
		"INSERT INTO default.visits SELECT number FROM numbers(1000);")
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
	sql("INSERT INTO shop.notes VALUES (3, 'c'); CREATE DATABASE later; CREATE TABLE later.things (id UInt64) ENGINE = MergeTree ORDER BY id;")
	second := test.copyNow()
	test.encryptedForBoth(second)

	// The copy in the bucket is a zip, and nothing of it stays in the
	// ClickHouse.
	from := mustCopy(t, test, second.ID)
	if from.size == 0 {
		t.Fatal("the copy carries no note of its size")
	}
	start := make([]byte, 4)
	if _, err := from.copy.Read(start); err != nil || string(start) != "PK\x03\x04" {
		t.Fatalf("the copy is no zip: %q %v", start, err)
	}
	if out, _ := exec.Command("docker", "exec", target.container, "ls", "/var/lib/clickhouse/backups").Output(); len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("a copy left its zip in the ClickHouse: %s", out)
	}

	if state := test.restore(protocol.BackupRestore{Copy: first.ID}); state.State != protocol.RestoreDone {
		t.Fatalf("restore to the first copy: %+v", state)
	}
	if got := rows(); got != "a,b" || !test.running() {
		t.Fatalf("after the restore: %q, running %v", got, test.running())
	}
	// The other tables and the view came back, the view still counts, and
	// a database made after the copy is gone.
	sql("INSERT INTO shop.notes VALUES (9, 'a'); ALTER TABLE shop.notes DELETE WHERE id = 9 SETTINGS mutations_sync = 1;")
	if got := sql("SELECT (SELECT count() FROM default.visits), (SELECT id FROM shop.`odd ``name`), (SELECT sum(times) FROM shop.counted WHERE body = 'a'), (SELECT count() FROM system.databases WHERE name = 'later');"); got != "1000\t7\t2\t0" {
		t.Fatalf("after the restore: %q", got)
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

	// The daily check passes a good copy and leaves no container.
	if why := test.check(); why != "" {
		t.Fatalf("the check failed: %s", why)
	}
	if out, _ := exec.Command("docker", "ps", "-a", "-q", "--filter", "name="+tempName("check-")).Output(); len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("the check left its container: %s", out)
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
		if _, err := test.b.apps.stopServices(test.ctx, app, test.b.otherServices(app, "events")...); err != nil {
			t.Fatal(err)
		}
		mark := test.killed()
		if step == "halfway" {
			if err := engine.keep(test.ctx, mark.Undo); err != nil {
				t.Fatal(err)
			}
			sql("DROP DATABASE later SYNC; TRUNCATE TABLE shop.notes;")
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
	if listed, err := test.listed(); err != nil || len(listed) != 1 || listed[0].Database != "clickhouse-events" || listed[0].Kind != "clickhouse" {
		t.Fatalf("listed %+v %v", listed, err)
	}
}
