package watcher

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/internal/report"
)

var t0 = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

func TestSilent(t *testing.T) {
	started := t0.Add(-time.Hour)
	cases := []struct {
		name     string
		received time.Time
		started  time.Time
		now      time.Time
		every    time.Duration
		want     bool
	}{
		{"just reported", t0, started, t0.Add(time.Minute), time.Minute, false},
		{"three intervals is not yet", t0, started, t0.Add(3 * time.Minute), time.Minute, false},
		{"more than three", t0, started, t0.Add(3*time.Minute + time.Second), time.Minute, true},
		{"its own interval", t0, started, t0.Add(20 * time.Minute), 10 * time.Minute, false},
		{"no interval means a minute", t0, started, t0.Add(4 * time.Minute), 0, true},
		{"never reported", time.Time{}, started, t0, time.Minute, false},
		{"watcher was off", t0, t0.Add(time.Hour), t0.Add(time.Hour + time.Minute), time.Minute, false},
		{"watcher was off, still silent", t0, t0.Add(time.Hour), t0.Add(time.Hour + 4*time.Minute), time.Minute, true},
	}
	for _, c := range cases {
		if got := silent(c.received, c.started, c.now, c.every); got != c.want {
			t.Errorf("%s: silent = %v, want %v", c.name, got, c.want)
		}
	}
}

// Three visits in a row turn a domain; one in between starts the count over.
func TestDomainInARow(t *testing.T) {
	d := &domain{}
	steps := []struct {
		up   bool
		down bool
	}{
		{false, false}, {false, false}, {true, false}, // a hiccup
		{false, false}, {false, false}, {false, true}, // down
		{true, true}, {false, true}, {true, true}, {true, true}, {true, false}, // back
	}
	for i, step := range steps {
		d.visited(step.up, "connection refused", t0.Add(time.Duration(i)*time.Minute))
		if d.Down != step.down {
			t.Fatalf("after visit %d: down = %v, want %v", i, d.Down, step.down)
		}
	}
	if !d.Since.Equal(t0.Add(8 * time.Minute)) {
		t.Errorf("since = %v, want the first of the three visits", d.Since)
	}
}

func testReport(apps ...report.App) report.Report {
	return report.Report{Server: "prod", Version: "v1.0.0", Every: time.Minute, Apps: apps}
}

var shop = report.App{Name: "shop", Release: 3, Running: true, Domains: []string{"shop.example.com"}}

// Each event alerts once: silence, back, down, up.
func TestAlertsOnce(t *testing.T) {
	s := newState()
	started := t0.Add(-time.Hour)
	s.receive("prod", testReport(shop), t0)
	if got := s.alerts(started, t0); len(got) != 0 {
		t.Fatalf("alerts after a report = %v", got)
	}

	at := t0.Add(4 * time.Minute)
	got := s.alerts(started, at)
	if len(got) != 1 || got[0].Title != "prod stopped reporting" || got[0].OK {
		t.Fatalf("alerts when silent = %+v", got)
	}
	if got := s.alerts(started, at.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("silence alerted twice: %v", got)
	}

	// A domain of a silent server does not alert.
	d := s.Domains["shop.example.com"]
	for i := range 3 {
		d.visited(false, "connection refused", at.Add(time.Duration(i)*time.Minute))
	}
	if got := s.alerts(started, at.Add(3*time.Minute)); len(got) != 0 {
		t.Fatalf("a silent server's domain alerted: %v", got)
	}

	// Back: the server reports again, and only now the domain alerts.
	back := t0.Add(10 * time.Minute)
	s.receive("prod", testReport(shop), back)
	got = s.alerts(started, back)
	if len(got) != 2 || got[0].Title != "prod reports again" || !got[0].OK || got[1].Title != "shop.example.com is down" {
		t.Fatalf("alerts when back = %+v", got)
	}
	if got := s.alerts(started, back); len(got) != 0 {
		t.Fatalf("alerted twice: %v", got)
	}

	for i := range 3 {
		d.visited(true, "", back.Add(time.Duration(i)*time.Minute))
	}
	got = s.alerts(started, back.Add(3*time.Minute))
	if len(got) != 1 || got[0].Title != "shop.example.com is up again" || !got[0].OK || got[0].App != "shop" {
		t.Fatalf("alerts when up = %+v", got)
	}
	if len(s.Outages) != 2 || s.Outages[0].To.IsZero() || s.Outages[1].To.IsZero() {
		t.Errorf("outages = %+v, want two that ended", s.Outages)
	}
}

// Only running apps' domains are visited; a domain that went away is forgotten.
func TestSyncDomains(t *testing.T) {
	s := newState()
	stopped := report.App{Name: "blog", Domains: []string{"blog.example.com"}}
	s.receive("prod", testReport(shop, stopped), t0)
	if s.Domains["shop.example.com"] == nil || s.Domains["blog.example.com"] != nil {
		t.Fatalf("domains = %v", s.Domains)
	}
	s.receive("prod", testReport(), t0.Add(time.Minute))
	if len(s.Domains) != 0 {
		t.Errorf("domains = %v, want none", s.Domains)
	}
}

// A restore check goes into the history once, when it is new.
func TestReceiveChecks(t *testing.T) {
	s := newState()
	app := shop
	app.Databases = []report.Database{{Name: "main", Kind: "postgres", LastCheck: t0, CheckOK: true, CheckedCopy: t0.Add(-time.Hour)}}
	s.receive("prod", testReport(app), t0)
	s.receive("prod", testReport(app), t0.Add(time.Minute))
	if len(s.Checks) != 1 || !s.Checks[0].OK {
		t.Fatalf("checks = %+v", s.Checks)
	}
	app.Databases = []report.Database{{Name: "main", Kind: "postgres", LastCheck: t0.Add(24 * time.Hour), Error: "no table users"}}
	s.receive("prod", testReport(app), t0.Add(24*time.Hour))
	if len(s.Checks) != 2 || s.Checks[1].OK || s.Checks[1].Error != "no table users" {
		t.Fatalf("checks = %+v", s.Checks)
	}
}

func testWatcher(t *testing.T) *watcher {
	t.Helper()
	config := Config{Password: "hunter2", Servers: map[string]string{"prod": "prod-token-0123456789", "web-1": "web-token-0123456789"}}
	w, err := newWatcher(config, t.TempDir(), t0)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func post(t *testing.T, h http.Handler, token, body string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, report.Path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestReceiveAuth(t *testing.T) {
	w := testWatcher(t)
	h := w.routes()
	body := `{"server":"prod","every":60000000000,"apps":[{"name":"shop","running":true,"domains":["shop.example.com"]}]}`
	if code := post(t, h, "", body); code != http.StatusUnauthorized {
		t.Errorf("no token: %d", code)
	}
	if code := post(t, h, "wrong-token-0123456789", body); code != http.StatusUnauthorized {
		t.Errorf("unknown token: %d", code)
	}
	if code := post(t, h, "web-token-0123456789", body); code != http.StatusBadRequest {
		t.Errorf("another server's token: %d", code)
	}
	if code := post(t, h, "prod-token-0123456789", "{"); code != http.StatusBadRequest {
		t.Errorf("not JSON: %d", code)
	}
	if code := post(t, h, "prod-token-0123456789", body); code != http.StatusNoContent {
		t.Errorf("good report: %d", code)
	}
	if w.state.Servers["prod"] == nil || w.state.Domains["shop.example.com"] == nil {
		t.Errorf("state = %+v", w.state)
	}
}

func TestPage(t *testing.T) {
	w := testWatcher(t)
	h := w.routes()
	post(t, h, "prod-token-0123456789", `{"server":"prod","version":"v1.0.0","every":60000000000,"apps":[{"name":"shop","release":3,"running":true,"domains":["shop.example.com"]}]}`)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("without password: %d", recorder.Code)
	}

	request.SetBasicAuth("", "hunter2")
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	page := recorder.Body.String()
	for _, want := range []string{"prod", "shop", "v3", "shop.example.com", "web-1", "no report yet"} {
		if !strings.Contains(page, want) {
			t.Errorf("page has no %q", want)
		}
	}
	if strings.Contains(page, "<form") || strings.Contains(page, "<button") || strings.Contains(page, "<script") {
		t.Error("the page must not change anything")
	}

	request = httptest.NewRequest(http.MethodPost, "/", nil)
	request.SetBasicAuth("", "hunter2")
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d", recorder.Code)
	}
}

// The state survives a restart, and a server gone from the config is forgotten.
func TestStateSaved(t *testing.T) {
	dir := t.TempDir()
	config := Config{Password: "x", Servers: map[string]string{"prod": "prod-token-0123456789"}}
	w, err := newWatcher(config, dir, t0)
	if err != nil {
		t.Fatal(err)
	}
	post(t, w.routes(), "prod-token-0123456789", `{"server":"prod","apps":[{"name":"shop","running":true,"domains":["shop.example.com"]}]}`)
	w.check(t.Context())
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	again, err := newWatcher(config, dir, t0)
	if err != nil || again.state.Servers["prod"] == nil {
		t.Fatalf("after restart: %v, %+v", err, again.state)
	}
	gone, err := newWatcher(Config{Password: "x"}, dir, t0)
	if err != nil || len(gone.state.Servers) != 0 || len(gone.state.Domains) != 0 {
		t.Fatalf("after removal from config: %v, %+v", err, gone.state)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(text string) string {
		path := filepath.Join(dir, "watcher.yml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	config, err := LoadConfig(write("password: p\nservers:\n  prod: 0123456789abcdef0\nalerts:\n  - kind: webhook\n    url: https://example.com/hook\n"))
	if err != nil || config.Servers["prod"] == "" || config.Alerts[0].Kind != "webhook" {
		t.Fatalf("config = %+v, %v", config, err)
	}
	for _, bad := range []string{
		"servers: {}\n",
		"password: p\nservers:\n  prod: short\n",
		"password: p\nservers:\n  prod: 0123456789abcdef0\n  web-1: 0123456789abcdef0\n",
		"password: p\nservers:\n  'a b': 0123456789abcdef0\n",
		"password: p\nunknown: 1\n",
	} {
		if _, err := LoadConfig(write(bad)); err == nil {
			t.Errorf("LoadConfig(%q) = nil, want an error", bad)
		}
	}
}
