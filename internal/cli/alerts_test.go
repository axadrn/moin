package cli

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAlerts(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/account":              `{"user":{"email":"ada@example.com","name":"Ada","email_alerts":true}}`,
		"PUT /api/v1/account/email-alerts": `{"on":false}`,
		"GET /api/v1/alert-destinations":   `{"items":[{"name":"team","kind":"slack","hint":"hooks.slack.com/…WXYZ","events":["uptime.down","uptime.up","server.down","server.up","deploy.failed"],"failing":true,"last_error":"HTTP 404: no_service"}]}`,
	}}
	code, stdout, _ := runCLI(t, api, "alerts")
	if code != 0 || !strings.Contains(stdout, "Email alerts to ada@example.com: on") || !strings.Contains(stdout, "hooks.slack.com/…WXYZ") || !strings.Contains(stdout, "failing: HTTP 404: no_service") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
	code, _, _ = runCLI(t, api, "alerts", "off")
	if code != 0 || !api.seen(`PUT /api/v1/account/email-alerts {"on":false}`) {
		t.Fatalf("code %d, requests %v", code, api.requests)
	}
	if code, _, _ = runCLI(t, api, "alerts", "maybe"); code == 0 {
		t.Fatal("alerts maybe worked")
	}
}

// runCLIWith runs the CLI with stdin, against api.
func runCLIWith(t *testing.T, api *fakeAPI, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPS_URL", server.URL)
	t.Setenv("OPS_TOKEN", "ops_test")
	t.Setenv("NO_COLOR", "1")
	var out, errOut strings.Builder
	code = Run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestAlertDestinations: add reads the URL or bot token on stdin, a
// webhook prints its signing secret once, test and rm name the destination.
func TestAlertDestinations(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"POST /api/v1/alert-destinations":           `{"name":"ci","kind":"webhook","events":["deploy.failed"],"signing_secret":"f00dfeed"}`,
		"POST /api/v1/alert-destinations/ci/test":   `{"name":"ci","kind":"webhook"}`,
		"DELETE /api/v1/alert-destinations/ci":      ``,
		"POST /api/v1/alert-destinations/team/test": `{"name":"team"}`,
	}}
	code, stdout, stderr := runCLIWith(t, api, "https://ci.example.com/hook\n", "alerts", "add", "webhook", "ci", "--events", "deploy.failed")
	if code != 0 || !strings.Contains(stdout, "Signing secret, shown this once: f00dfeed") || !strings.Contains(stdout, "ops alerts test ci") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !api.seen(`POST /api/v1/alert-destinations {"kind":"webhook","name":"ci","url":"https://ci.example.com/hook","events":["deploy.failed"]}`) {
		t.Fatalf("requests %v", api.requests)
	}
	code, _, _ = runCLIWith(t, api, "123456:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawQ\n", "alerts", "add", "telegram", "phone", "--chat", "-1001234")
	if code != 0 || !api.seen(`POST /api/v1/alert-destinations {"kind":"telegram","name":"phone","bot_token":"123456:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawQ","chat_id":"-1001234"}`) {
		t.Fatalf("code %d, requests %v", code, api.requests)
	}
	if code, _, stderr = runCLIWith(t, api, "token\n", "alerts", "add", "telegram", "phone"); code == 0 || !strings.Contains(stderr, "--chat") {
		t.Fatalf("telegram without --chat: code %d, stderr %q", code, stderr)
	}
	if code, stdout, _ = runCLI(t, api, "alerts", "test", "ci"); code != 0 || !strings.Contains(stdout, "Sent a test to ci") {
		t.Fatalf("test: code %d, stdout %q", code, stdout)
	}
	if code, _, _ = runCLI(t, api, "alerts", "rm", "ci", "--yes"); code != 0 || !api.seen("DELETE /api/v1/alert-destinations/ci") {
		t.Fatalf("rm: code %d, requests %v", code, api.requests)
	}
	if code, _, _ = runCLI(t, api, "alerts", "rm", "ci"); code == 0 {
		t.Fatal("rm without --yes off a terminal worked")
	}
}
