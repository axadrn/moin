package cli

import (
	"strings"
	"testing"
)

func TestMetrics(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/apps/shop/metrics": `{"app":"shop","server":"web-1","server_online":true,"range":"1h","step_seconds":15,
			"now":{"at":"2026-11-03T12:00:00Z","cpu":12.5,"memory_used":314572800,"memory_limit":536870912,"net_rx":2048,"net_tx":1024,"containers":1},
			"points":[{"at":"2026-11-03T11:59:45Z","cpu":10,"memory":209715200,"net_rx":0,"net_tx":0},{"at":"2026-11-03T12:00:00Z","cpu":12.5,"memory":314572800,"net_rx":2048,"net_tx":1024}],
			"deploys":[{"number":12,"at":"2026-11-03T11:30:00Z"}]}`,
		"GET /api/v1/servers/web-1/metrics": `{"server":"web-1","online":true,"range":"24h","step_seconds":300,
			"now":{"at":"2026-11-03T12:00:00Z","cpu":42,"cores":4,"memory_used":1073741824,"memory_total":4294967296,"disk_used":10737418240,"disk_total":42949672960,"net_rx":1000,"net_tx":500,"load":0.5},
			"points":[{"at":"2026-11-03T11:55:00Z","cpu":40,"memory":1073741824,"disk":10737418240,"net_rx":1,"net_tx":1,"load":0.4}],
			"apps":[{"app":"shop","now":{"cpu":5,"memory_used":314572800,"net_rx":1,"net_tx":1,"containers":1}},{"app":"blog","now":null}]}`,
	}}
	code, stdout, stderr := runCLI(t, api, "metrics", "shop")
	for _, want := range []string{"shop", "on web-1", "12.5%", "300 MB of 512 MB", "2.0 KB/s", "v12 Nov 3 11:30", "▁█"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("no %q in %q (code %d, stderr %q)", want, stdout, code, stderr)
		}
	}
	if !api.seen("GET /api/v1/apps/shop/metrics range=1h") {
		t.Errorf("requests = %v", api.requests)
	}
	code, stdout, stderr = runCLI(t, api, "metrics", "--server", "web-1", "--range", "24h")
	for _, want := range []string{"web-1", "42.0%", "4 cores", "1.0 GB of 4.0 GB", "10 GB of 40 GB", "25%", "APP", "shop", "not running"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("no %q in %q (code %d, stderr %q)", want, stdout, code, stderr)
		}
	}
	// A name that is no app is tried as a server.
	if code, stdout, _ := runCLI(t, api, "metrics", "web-1"); code != 0 || !strings.Contains(stdout, "4 cores") {
		t.Errorf("metrics web-1: code %d, stdout %q", code, stdout)
	}
	if code, _, stderr := runCLI(t, api, "metrics", "nope"); code == 0 || !strings.Contains(stderr, "nope") {
		t.Errorf("metrics nope: code %d, stderr %q", code, stderr)
	}
	if code, _, _ := runCLI(t, api, "metrics", "--server", "web-1", "shop"); code == 0 {
		t.Error("a server and an app ran")
	}
}
