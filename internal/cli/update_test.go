package cli

import (
	"strings"
	"testing"
)

func TestVersionSaysANewerReleaseIsOut(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/version": `{"version":"v0.2.0","self_hosted":true,"latest":"v0.3.0","update_available":true,"notes_url":"https://github.com/axadrn/moin/releases/tag/v0.3.0","summary":"One-click updates.","can_update":true,"updater":true,"update":null}`,
	}}
	code, stdout, stderr := runCLI(t, api, "version")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"hub v0.2.0", "moin v0.3.0 is out. One-click updates.", "releases/tag/v0.3.0", "moin update"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q misses %q", stdout, want)
		}
	}
}

func TestUpdateOnTheHostedHub(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/version": `{"version":"v0.3.0","self_hosted":false,"update":null}`,
	}}
	code, stdout, _ := runCLI(t, api, "update", "--yes")
	if code != 0 || !strings.Contains(stdout, "updates itself with every release") || api.seen("POST /api/v1/version/update") {
		t.Fatalf("code %d, stdout %q, requests %v", code, stdout, api.requests)
	}
}

func TestDaemonAutoUpdate(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/daemon-updates": `{"auto":true}`,
		"PUT /api/v1/daemon-updates": `{"auto":false}`,
	}}
	code, stdout, _ := runCLI(t, api, "servers", "auto-update")
	if code != 0 || !strings.Contains(stdout, "by themselves: on") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
	code, _, _ = runCLI(t, api, "servers", "auto-update", "off")
	if code != 0 || !api.seen(`PUT /api/v1/daemon-updates {"auto":false}`) {
		t.Fatalf("code %d, requests %v", code, api.requests)
	}
}

// TestUpdateShowsTheNotesFirst: the whole release notes and the line above
// them come before the question, and nothing updates without an answer.
func TestUpdateShowsTheNotesFirst(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/version": `{"version":"v0.2.0","self_hosted":true,"latest":"v0.3.0","update_available":true,"summary":"One-click updates.","notes":"**In short:** One-click updates.\n\n### Upgrade\n\nChange your tunnel first.","read_first":"Read this first. Some updates need a step from you before you update.","can_update":true,"updater":true,"update":null}`,
	}}
	code, stdout, _ := runCLI(t, api, "update")
	if code == 0 || api.seen("POST /api/v1/version/update") {
		t.Fatalf("updated without an answer: code %d, requests %v", code, api.requests)
	}
	for _, want := range []string{"moin v0.3.0", "Read this first.", "Change your tunnel first."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q misses %q", stdout, want)
		}
	}
}
