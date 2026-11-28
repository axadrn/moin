package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/internal/daemon"
)

// The list says for each deploy where it came from, what it ran and how
// it ended; a failed newest one names the command that shows why.
func TestPrintDeploys(t *testing.T) {
	now := time.Now()
	list := []daemon.DeployRecord{
		{Number: 4, At: now.Add(-time.Minute), Ended: now.Add(-time.Minute + 38*time.Second), Status: "failed", Source: "follow", Commit: "3f2a9c1e0b4d"},
		{Number: 3, At: now.Add(-time.Hour), Status: "ok", Live: true, Source: "rollback", From: 1, Commit: "aaaaaaa1"},
		{Number: 2, At: now.Add(-2 * time.Hour), Status: "ok", Source: "laptop", Image: "ghcr.io/ada/shop:1.4", ImageID: "9b1c2d3e4f5a"},
		{Number: 1, At: now.Add(-3 * time.Hour)},
	}
	var out strings.Builder
	printDeploys(&out, "shop", list, now)
	text := out.String()
	for _, want := range []string{"v4", "follow", "3f2a9c1", "failed", "38s", "rollback to v1", "live", "shop:1.4 9b1c2d3", "no record", "deploys shop v4"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}
}

// A deploy's log comes after one line that says what it was. One from
// before logs says it has none.
func TestPrintDeployLog(t *testing.T) {
	var out strings.Builder
	printDeployLog(&out, daemon.DeployLog{DeployRecord: daemon.DeployRecord{Number: 5, Status: "ok", Source: "laptop", Catalog: "umami"}, Log: "starting\nv5 is live\n"})
	if text := out.String(); !strings.Contains(text, " · laptop · catalog umami · ok\nstarting\n") || !strings.HasSuffix(text, "v5 is live\n") {
		t.Errorf("log:\n%s", text)
	}
	out.Reset()
	printDeployLog(&out, daemon.DeployLog{DeployRecord: daemon.DeployRecord{Number: 1}})
	if text := out.String(); !strings.Contains(text, "no record") || !strings.Contains(text, "No log") {
		t.Errorf("old one:\n%s", text)
	}
}
