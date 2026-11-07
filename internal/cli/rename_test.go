package cli

import (
	"strings"
	"testing"
)

// TestAppsRename: apps rename asks the hub to rename the app by name, ID
// or link and says what stays and what to do next.
func TestAppsRename(t *testing.T) {
	const id = "0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b"
	answer := `{"id":"` + id + `","name":"blog","server_name":"web-1"}`
	for _, from := range []string{"my-app", id, "my-app-" + id} {
		api := &fakeAPI{answers: map[string]string{"PATCH /api/v1/apps/" + from: answer}}
		code, stdout, stderr := runCLI(t, api, "apps", "rename", from, "blog")
		if code != 0 || !api.seen(`PATCH /api/v1/apps/`+from+` {"name":"blog"}`) || !strings.Contains(stdout, "Renamed "+from+" to blog") || !strings.Contains(stdout, "moin restart blog") || !strings.Contains(stdout, "x-moin.app: blog") {
			t.Errorf("%s: code %d, stdout %q, stderr %q, requests %v", from, code, stdout, stderr, api.requests)
		}
	}
	code, _, stderr := runCLI(t, &fakeAPI{}, "apps", "rename", "my-app")
	if code == 0 || !strings.Contains(stderr, "apps rename <app> <new name>") {
		t.Errorf("one argument: code %d, stderr %q", code, stderr)
	}
}
