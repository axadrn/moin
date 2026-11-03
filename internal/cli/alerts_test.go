package cli

import (
	"strings"
	"testing"
)

func TestAlerts(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/account":              `{"user":{"email":"ada@example.com","name":"Ada","email_alerts":true}}`,
		"PUT /api/v1/account/email-alerts": `{"on":false}`,
	}}
	code, stdout, _ := runCLI(t, api, "alerts")
	if code != 0 || !strings.Contains(stdout, "Email alerts to ada@example.com: on") {
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
