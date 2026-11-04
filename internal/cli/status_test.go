package cli

import (
	"strings"
	"testing"
)

// TestStatus: moin status exits 0 when all is good and 1 when something
// needs you, so scripts and CI can check it.
func TestStatus(t *testing.T) {
	good := &fakeAPI{answers: map[string]string{
		"GET /api/v1/status": `{"ok":true,"issues":[],"updates":[{"app":"stats","name":"Umami","version":"3.4.1","latest":"3.5.0"}],"counts":{"apps":2,"apps_live":2,"servers":1,"servers_online":1,"databases":2,"backups_checked":2}}`,
	}}
	code, stdout, stderr := runCLI(t, good, "status")
	if code != 0 {
		t.Fatalf("all good: code %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"All good.", "2 of 2 apps live", "stats", "3.4.1 → 3.5.0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("no %q in %q", want, stdout)
		}
	}
	bad := &fakeAPI{answers: map[string]string{
		"GET /api/v1/status": `{"ok":false,"issues":[{"kind":"server.down","target":"web-1","detail":"offline: its apps may be down","path":"/servers/x/security","since":null},{"kind":"agent.behind","target":"web-2","detail":"agent v1.0.0, update pending","path":"/servers","since":null}],"updates":[],"counts":{"apps":1,"apps_live":0,"servers":2,"servers_online":1,"databases":0,"backups_checked":0}}`,
	}}
	code, stdout, _ = runCLI(t, bad, "status")
	if code != 1 {
		t.Errorf("needs you: code %d", code)
	}
	for _, want := range []string{"2 things need you", "web-1", "offline: its apps may be down", "web-2"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("no %q in %q", want, stdout)
		}
	}
	if code, _, _ := runCLI(t, good, "status", "extra"); code == 0 {
		t.Error("status with an argument ran")
	}
}
