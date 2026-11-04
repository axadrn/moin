package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/internal/client"
)

// u and :uptime open the uptime prompt of the selected app.
func TestUptimeKeyOpensThePrompt(t *testing.T) {
	if m := press(frames()["apps"], "u"); m.mode != modeUptime {
		t.Errorf("u: mode %v", m.mode)
	}
	if m := command(frames()["apps"], "uptime"); m.mode != modeUptime {
		t.Errorf(":uptime: mode %v, err %v", m.mode, m.err)
	}
	if m := press(frames()["apps"], "u", "esc"); m.mode != modeList {
		t.Errorf("esc keeps the prompt open")
	}
}

func TestUptimeOutput(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	full, ended := 100.0, now.Add(-time.Hour)
	output := uptimeOutput(1, client.Uptime{App: "shop", State: "up", Server: "web-1", Domains: []client.UptimeDomain{
		{Domain: "shop.de", State: "up", Path: "/health", Uptime24h: &full, CheckedAt: &now, StatusCode: 200, ResponseMS: 84,
			Outages: []client.Outage{{StartedAt: now.Add(-2 * time.Hour), EndedAt: &ended, Reason: "HTTP 502"}}},
	}}, now)
	text := strings.Join(output.lines, "\n")
	for _, want := range []string{"shop.de", "100%", "200 in 84 ms", "/health", "last outages", "HTTP 502"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in %s", want, text)
		}
	}
	if !strings.Contains(output.note, "web-1 is offline") {
		t.Errorf("note = %q", output.note)
	}
}
