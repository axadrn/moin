package agent

import (
	"errors"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// TestBackupHealth: a failure counts only after failGrace, so a database
// restarting with a deploy tells nothing; the agent tells a change once,
// and once more when it works again. An urgent failure counts at once.
func TestBackupHealth(t *testing.T) {
	b := newBackups(newApps(t.TempDir(), func(protocol.MessageType, string, any) {}), func(protocol.MessageType, string, any) {})
	pg := protocol.Database{Path: "postgres-db", Kind: protocol.DatabasePostgres, Service: "db"}
	k := key("shop", pg)
	now := time.Now()

	if b.changed("shop", pg, now) {
		t.Error("a healthy database seen first is a change")
	}
	b.fail(k, errors.New("upload: access denied"))
	if got := b.problem("shop", pg, now); got != "" {
		t.Errorf("within the grace time: problem %q", got)
	}
	if b.changed("shop", pg, now) {
		t.Error("a failure within the grace time is a change")
	}
	later := now.Add(failGrace + time.Second)
	if got := b.problem("shop", pg, later); got != "upload: access denied" {
		t.Errorf("after the grace time: problem %q", got)
	}
	if !b.changed("shop", pg, later) {
		t.Error("a lasting failure is no change")
	}
	if b.changed("shop", pg, later.Add(time.Minute)) {
		t.Error("the same failure a minute later is told again")
	}
	b.fail(k, nil)
	if !b.changed("shop", pg, later.Add(2*time.Minute)) {
		t.Error("working again is no change")
	}

	// The history lost its place: it counts at once.
	b.fail(k, urgent{errors.New("the history lost its place")})
	if got := b.problem("shop", pg, time.Now()); got != "the history lost its place" {
		t.Errorf("an urgent failure: problem %q", got)
	}
	b.fail(k, nil)

	// A failed daily check is a change of its own.
	b.checked[k] = check{at: now, err: "the daily check failed: checksum"}
	if !b.changed("shop", pg, later.Add(20*time.Minute)) {
		t.Error("a failed daily check is no change")
	}
}
