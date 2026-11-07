package cli

import (
	"strings"
	"testing"
)

func TestUptime(t *testing.T) {
	api := &fakeAPI{answers: map[string]string{
		"GET /api/v1/apps/shop/uptime": `{"app":"shop","state":"down","server":"web-1","server_online":false,"domains":[
			{"domain":"shop.de","path":"/health","state":"up","visited_at":"2026-11-03T12:00:00Z","status_code":200,"response_ms":84,"uptime_24h":100,"uptime_7d":99.5,"uptime_30d":null,"outages":[]},
			{"domain":"www.shop.de","path":"/","state":"down","error":"HTTP 502","outages":[{"started_at":"2026-11-03T10:00:00Z","ended_at":"2026-11-03T10:30:00Z","reason":"HTTP 502"}]}]}`,
		"PUT /api/v1/apps/shop/uptime/shop.de": `{"domain":"shop.de","path":"/","paused":true,"state":"paused","outages":[]}`,
	}}
	code, stdout, stderr := runCLI(t, api, "uptime", "shop")
	for _, want := range []string{"shop.de", "100%", "99.50%", "200 in 84 ms", "/health", "Last outages", "HTTP 502", "web-1 is offline"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("no %q in %q (code %d, stderr %q)", want, stdout, code, stderr)
		}
	}
	if code, _, stderr := runCLI(t, api, "uptime", "path", "--app", "shop", "shop.de", "default"); code != 0 || !api.seen(`PUT /api/v1/apps/shop/uptime/shop.de {"path":""}`) {
		t.Errorf("path default: code %d, stderr %q, requests %v", code, stderr, api.requests)
	}
	if code, stdout, _ := runCLI(t, api, "uptime", "pause", "shop.de", "--app", "shop"); code != 0 || !api.seen(`PUT /api/v1/apps/shop/uptime/shop.de {"paused":true}`) || !strings.Contains(stdout, "Paused") {
		t.Errorf("pause: code %d, stdout %q", code, stdout)
	}
	if code, _, _ := runCLI(t, api, "uptime", "path", "--app", "shop", "shop.de"); code == 0 {
		t.Error("uptime path without a path ran")
	}
}
