package agent

import (
	"testing"
	"time"

	"github.com/axadrn/ops/protocol"
)

// TestBackupHealth: a failure counts only after failGrace, so a database
// restarting with a deploy tells nothing; the agent tells a change once,
// and once more when it works again. A broken WAL-G counts at once.
func TestBackupHealth(t *testing.T) {
	b := newBackups(newApps(t.TempDir(), func(protocol.MessageType, string, any) {}), func(protocol.MessageType, string, any) {})
	pg := protocol.Database{Path: "postgres-db", Kind: protocol.DatabasePostgres, Service: "db"}
	key := drillKey("shop", pg)
	now := time.Now()

	if b.changed("shop", pg, now) {
		t.Error("a healthy database seen first is a change")
	}
	b.fail(key, "snapshot", "hourly snapshot: upload: access denied")
	if got := b.problem("shop", pg, now); got != "" {
		t.Errorf("within the grace time: problem %q", got)
	}
	if b.changed("shop", pg, now) {
		t.Error("a failure within the grace time is a change")
	}
	later := now.Add(failGrace + time.Second)
	if got := b.problem("shop", pg, later); got != "hourly snapshot: upload: access denied" {
		t.Errorf("after the grace time: problem %q", got)
	}
	if !b.changed("shop", pg, later) {
		t.Error("a lasting failure is no change")
	}
	if b.changed("shop", pg, later.Add(time.Minute)) {
		t.Error("the same failure a minute later is told again")
	}
	b.fail(key, "snapshot", "")
	if !b.changed("shop", pg, later.Add(2*time.Minute)) {
		t.Error("working again is no change")
	}

	// The continuous backup working again does not clear a snapshot failure.
	b.fail(key, "snapshot", "hourly snapshot: boom")
	b.fail(key, "continuous", "")
	if got := b.problem("shop", pg, later.Add(10*time.Minute)); got != "hourly snapshot: boom" {
		t.Errorf("problem %q", got)
	}
	b.fail(key, "snapshot", "")

	// WAL-G lost its place in the log: it counts at once, until the new
	// base backup is done.
	state := b.walgState(key)
	state.broke, state.why = now, walgWhyLost
	if got := b.problem("shop", pg, now); got == "" {
		t.Error("a broken WAL-G is no problem")
	}
	state.rebased = now.Add(time.Minute)
	if got := b.problem("shop", pg, now.Add(2*time.Minute)); got != "" {
		t.Errorf("after the new base backup: problem %q", got)
	}

	// A failed restore check is a change of its own.
	b.verified[key] = drill{at: now, err: "restore drill failed: checksum"}
	if !b.changed("shop", pg, later.Add(20*time.Minute)) {
		t.Error("a failed restore check is no change")
	}
}
