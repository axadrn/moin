package tui

import (
	"strings"
	"testing"

	"github.com/axadrn/ops/internal/client"
)

// TestStatusOutput: all good says so with the counts; trouble lists each
// issue and the title counts them.
func TestStatusOutput(t *testing.T) {
	good := statusOutput(1, client.Status{OK: true, Counts: client.StatusCounts{Apps: 3, AppsLive: 3, Servers: 1, ServersOnline: 1}})
	text := strings.Join(good.lines, "\n")
	if good.title != "status: all good" || !strings.Contains(text, "All good.") || !strings.Contains(text, "3 of 3 apps live") {
		t.Errorf("good: %s\n%s", good.title, text)
	}
	bad := statusOutput(2, client.Status{
		Issues:  []client.StatusIssue{{Kind: "deploy.failed", Target: "shop", Detail: "v4 failed, v3 still runs"}},
		Updates: []client.StatusUpdate{{App: "stats", Name: "Umami", Version: "3.4.1", Latest: "3.5.0"}},
	})
	text = strings.Join(bad.lines, "\n")
	if bad.title != "status: 1 need you" || !strings.Contains(text, "v4 failed, v3 still runs") || !strings.Contains(text, "3.4.1 → 3.5.0") {
		t.Errorf("bad: %s\n%s", bad.title, text)
	}
	if names := strings.Join(commandNames(), " "); !strings.Contains(names, "status") {
		t.Errorf(":status does not complete: %s", names)
	}
}
