package cli

import (
	"strings"
	"testing"
)

func TestVersionSaysANewerReleaseIsOut(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/version": `{"version":"v0.2.0","self_hosted":true,"latest":"v0.3.0","update_available":true,"notes_url":"https://github.com/axadrn/ops/releases/tag/v0.3.0","summary":"One-click updates.","can_update":true,"updater":true,"update":null}`,
	}}
	code, stdout, stderr := runCLI(t, api, "version")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"control plane v0.2.0", "ops v0.3.0 is out. One-click updates.", "releases/tag/v0.3.0", "ops update"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q misses %q", stdout, want)
		}
	}
}

func TestUpdateOnTheHostedControlPlane(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/version": `{"version":"v0.3.0","self_hosted":false,"update":null}`,
	}}
	code, stdout, _ := runCLI(t, api, "update", "--yes")
	if code != 0 || !strings.Contains(stdout, "updates itself with every release") || api.seen("POST /api/v1/version/update") {
		t.Fatalf("code %d, stdout %q, requests %v", code, stdout, api.requests)
	}
}

func TestAgentAutoUpdate(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/agent-updates": `{"auto":true}`,
		"PUT /api/v1/agent-updates": `{"auto":false}`,
	}}
	code, stdout, _ := runCLI(t, api, "servers", "auto-update")
	if code != 0 || !strings.Contains(stdout, "by themselves: on") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
	code, _, _ = runCLI(t, api, "servers", "auto-update", "off")
	if code != 0 || !api.seen(`PUT /api/v1/agent-updates {"auto":false}`) {
		t.Fatalf("code %d, requests %v", code, api.requests)
	}
}
