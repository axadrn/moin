package cli

import (
	"strings"
	"testing"
)

func TestVersionSaysANewerReleaseIsOut(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/version": `{"version":"v0.2.0","self_hosted":true,"latest":"v0.3.0","update_available":true,"notes_url":"https://github.com/axadrn/moin/releases/tag/v0.3.0","summary":"Faster deploys.","read_first":"Read this first. Some updates need a step from you before you update."}`,
	}}
	code, stdout, stderr := runCLI(t, api, "version")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"hub v0.2.0", "moin v0.3.0 is out. Faster deploys.", "releases/tag/v0.3.0", "Read this first.", "sh /opt/moin/update.sh"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q misses %q", stdout, want)
		}
	}
}

func TestVersionShowsTheHubBackupToItsAdmins(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/version": `{"version":"v0.3.0","self_hosted":false,"hub_backup":{"state":"late","bucket":"acme-hub","last_copy":"2026-01-02T03:04:05Z"}}`,
	}}
	code, stdout, _ := runCLI(t, api, "version")
	if code != 0 || !strings.Contains(stdout, "hub backup ✗ late · bucket acme-hub · last copy") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
}

// update without an app says how a self-hosted hub updates.
func TestUpdateWithoutAppSaysHowTheHubUpdates(t *testing.T) {
	code, _, stderr := runCLI(t, &fakeAPI{}, "update")
	if code == 0 || !strings.Contains(stderr, "update <app>") || !strings.Contains(stderr, "sh /opt/moin/update.sh") {
		t.Fatalf("code %d, stderr %q", code, stderr)
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
