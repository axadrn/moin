package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/axadrn/moin/internal/alert"
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
		every    int
		want     bool
	}{
		{"just reported", t0, started, t0.Add(time.Minute), 60, false},
		{"three intervals is not yet", t0, started, t0.Add(3 * time.Minute), 60, false},
		{"more than three", t0, started, t0.Add(3*time.Minute + time.Second), 60, true},
		{"its own interval", t0, started, t0.Add(20 * time.Minute), 600, false},
		{"no interval means a minute", t0, started, t0.Add(4 * time.Minute), 0, true},
		{"two seconds count as a minute", t0, started, t0.Add(time.Minute), 2, false},
		{"a day counts as an hour", t0, started, t0.Add(4 * time.Hour), 86400, true},
		{"never reported", time.Time{}, started, t0, 60, false},
		{"watcher was off", t0, t0.Add(time.Hour), t0.Add(time.Hour + time.Minute), 60, false},
		{"watcher was off, still silent", t0, t0.Add(time.Hour), t0.Add(time.Hour + 4*time.Minute), 60, true},
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
	return report.Report{Server: "prod", Version: "v1.0.0", EverySeconds: 60, Apps: apps}
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

func testSite(t *testing.T) *site {
	t.Helper()
	config := Config{Password: "hunter2", Servers: map[string]string{"prod": "prod-token-0123456789", "web-1": "web-token-0123456789"}}
	s, err := newSite(config, t.TempDir(), t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
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
	s := testSite(t)
	h := s.routes()
	body := `{"server":"prod","every_seconds":60,"apps":[{"name":"shop","running":true,"domains":["shop.example.com"]}]}`
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
	if state := s.watcher.state; state.Servers["prod"] == nil || state.Domains["shop.example.com"] == nil {
		t.Errorf("state = %+v", state)
	}
}

func TestPage(t *testing.T) {
	s := testSite(t)
	h := s.routes()
	post(t, h, "prod-token-0123456789", `{"server":"prod","version":"v1.0.0","every_seconds":60,"apps":[{"name":"shop","release":3,"running":true,"domains":["shop.example.com"],"databases":[{"name":"main","kind":"sqlite"}]}]}`)

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
	for _, want := range []string{"prod", "shop", "v3", "shop.example.com", "web-1 has not reported yet", "watcher connect web-1", "main (sqlite)", "has no copy yet"} {
		if !strings.Contains(page, want) {
			t.Errorf("page has no %q", want)
		}
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("the page may be framed")
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
	s, err := newSite(config, dir, t0)
	if err != nil {
		t.Fatal(err)
	}
	post(t, s.routes(), "prod-token-0123456789", `{"server":"prod","apps":[{"name":"shop","running":true,"domains":["shop.example.com"]}]}`)
	s.watcher.Tick(t.Context())
	s.save()
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	again, err := newSite(config, dir, t0)
	if err != nil || again.watcher.state.Servers["prod"] == nil {
		t.Fatalf("after restart: %v, %+v", err, again.watcher.state)
	}
	gone, err := newSite(Config{Password: "x"}, dir, t0)
	if err != nil || len(gone.watcher.state.Servers) != 0 || len(gone.watcher.state.Domains) != 0 {
		t.Fatalf("after removal from config: %v, %+v", err, gone.watcher.state)
	}
}

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watcher.yml")
	write := func(text string) string {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	const good = "password: a-long-test-password\nalerts:\n  - kind: webhook\n    url: https://example.com/hook\n"
	config, err := LoadConfig(write(good + "servers:\n  prod: test-token-of-prod\n"))
	if err != nil || config.Servers["prod"] == "" || config.Alerts[0].Kind != "webhook" {
		t.Fatalf("config = %+v, %v", config, err)
	}
	for _, bad := range []string{
		"alerts:\n  - kind: webhook\n    url: https://example.com/hook\n",
		"password: short\nalerts:\n  - kind: webhook\n    url: https://example.com/hook\n",
		"password: a-long-test-password\nalerts:\n  - kind: mail\n",
		"password: a-long-test-password\nalerts:\n  - kind: webhook\n    url: http://example.com/hook\n",
		good + "servers:\n  prod: short\n",
		good + "servers:\n  prod: test-token-of-prod\n  web-1: test-token-of-prod\n",
		good + "servers:\n  'a b': test-token-of-prod\n",
		good + "unknown: 1\n",
	} {
		if _, err := LoadConfig(write(bad)); err == nil {
			t.Errorf("LoadConfig(%q) = nil, want an error", bad)
		}
	}
	// The watcher's own alerts are optional: the servers bring theirs.
	if _, err := LoadConfig(write("password: a-long-test-password\n")); err != nil {
		t.Errorf("a config without alerts: %v", err)
	}
	write(good)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("a config others can read: %v", err)
	}
}

// A watcher that restarts does not call a silent server back, nor silent
// again; the next report does call it back.
func TestRestartKeepsSilence(t *testing.T) {
	dir := t.TempDir()
	config := Config{Password: "x", Servers: map[string]string{"prod": "prod-token-0123456789"}}
	s := newState()
	s.receive("prod", testReport(), t0)
	if got := s.alerts(t0, t0.Add(4*time.Minute)); len(got) != 1 {
		t.Fatalf("alerts = %v, want the silence", got)
	}
	saved, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), saved, 0o600); err != nil {
		t.Fatal(err)
	}
	restart := t0.Add(time.Hour)
	restarted, err := newSite(config, dir, restart)
	if err != nil {
		t.Fatal(err)
	}
	w := restarted.watcher
	for _, at := range []time.Duration{time.Minute, 4 * time.Minute} {
		if got := w.state.alerts(restart, restart.Add(at)); len(got) != 0 {
			t.Fatalf("after the restart: %v", got)
		}
	}
	w.state.receive("prod", testReport(), restart.Add(5*time.Minute))
	if got := w.state.alerts(restart, restart.Add(5*time.Minute)); len(got) != 1 || got[0].Title != "prod reports again" {
		t.Fatalf("alerts = %v, want back", got)
	}
}

// A report can only make the watcher visit plain public names.
func TestReportDomains(t *testing.T) {
	for _, d := range []string{"localhost:1", "127.0.0.1", "example.com/x", "ada@example.com", "[::1]", "localhost", "EXAMPLE.com", "-a.example.com"} {
		r := testReport(report.App{Name: "shop", Running: true, Domains: []string{d}})
		if err := checkReport(&r); err == nil {
			t.Errorf("checkReport took %q", d)
		}
	}
	r := testReport(report.App{Name: "shop", Running: true, Domains: []string{"shop.example.com", "shop.203-0-113-7.sslip.io"}})
	if err := checkReport(&r); err != nil {
		t.Error(err)
	}
	for _, address := range []string{"127.0.0.1:443", "10.0.0.5:443", "192.168.1.1:80", "100.100.1.1:443", "169.254.169.254:80", "0.0.0.0:80", "[::1]:443", "[fe80::1]:443"} {
		if err := RefusePrivate("tcp", address, nil); err == nil {
			t.Errorf("RefusePrivate let %s through", address)
		}
	}
	if err := RefusePrivate("tcp", "203.0.113.7:443", nil); err != nil {
		t.Errorf("RefusePrivate stopped a public address: %v", err)
	}
	local := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer local.Close()
	if _, err := visitClient().Get(local.URL); err == nil || visitFailure(err) != errPrivate.Error() {
		t.Errorf("a visit to loopback: %v", err)
	}
}

// A report too big or with odd names is refused; a long error is cut.
func TestReportLimits(t *testing.T) {
	many := testReport(make([]report.App, maxApps+1)...)
	if err := checkReport(&many); err == nil {
		t.Error("took too many apps")
	}
	for _, name := range []string{"", "a b", "../x"} {
		r := testReport(report.App{Name: name})
		if err := checkReport(&r); err == nil {
			t.Errorf("took app name %q", name)
		}
	}
	r := testReport(report.App{Name: "shop", Databases: []report.Database{{Name: "data/app.db", Error: strings.Repeat("ü", 300)}}})
	if err := checkReport(&r); err != nil {
		t.Fatal(err)
	}
	if got := r.Apps[0].Databases[0].Error; len(got) > maxError+len("…") || !utf8.ValidString(got) {
		t.Errorf("error not cut: %d bytes", len(got))
	}
}

// An alert a destination did not take is sent again on the next check.
func TestUnsentAgain(t *testing.T) {
	var calls, took int
	hook := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			rw.WriteHeader(http.StatusBadGateway)
			return
		}
		took++
	}))
	defer hook.Close()
	config := Config{Password: "x", Servers: map[string]string{"prod": "prod-token-0123456789"}, Alerts: []alert.Destination{{Kind: "webhook", URL: hook.URL}}}
	s, err := newSite(config, t.TempDir(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	w := s.watcher
	w.state.receive("prod", testReport(), time.Now().Add(-10*time.Minute))
	w.Tick(t.Context())
	if took != 0 || len(w.state.Unsent) != 1 {
		t.Fatalf("after a failure: took %d, unsent %d", took, len(w.state.Unsent))
	}
	w.Tick(t.Context())
	if took != 1 || len(w.state.Unsent) != 0 {
		t.Fatalf("after the retry: took %d, unsent %d", took, len(w.state.Unsent))
	}
}

// When two servers report one domain, the one that had it keeps it.
func TestDomainOwner(t *testing.T) {
	s := newState()
	s.receive("web-2", report.Report{Server: "web-2", Apps: []report.App{shop}}, t0)
	s.receive("web-1", report.Report{Server: "web-1", Apps: []report.App{shop}}, t0)
	if got := s.Domains["shop.example.com"].Server; got != "web-2" {
		t.Errorf("owner = %s, want web-2, which had it first", got)
	}
}

// The history drops the oldest outage that ended, never an open one.
func TestOutagesKeepOpen(t *testing.T) {
	s := newState()
	s.addOutage(outage{Server: "prod", From: t0})
	for i := range kept {
		s.addOutage(outage{Server: "web-1", From: t0, To: t0.Add(time.Duration(i+1) * time.Minute)})
	}
	if len(s.Outages) != kept || s.Outages[0].Server != "prod" || !s.Outages[0].To.IsZero() {
		t.Errorf("outages: %d, first %+v", len(s.Outages), s.Outages[0])
	}
}

// A state file that is not JSON says how to start over.
func TestCorruptState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newSite(Config{}, dir, t0); err == nil || !strings.Contains(err.Error(), "Delete it to start fresh") {
		t.Errorf("err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"servers":null,"domains":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := newSite(Config{}, dir, t0)
	if err != nil || s.watcher.state.Servers == nil || s.watcher.state.Domains == nil {
		t.Errorf("null maps: %v", err)
	}
}

// Receive takes a report only of a server the watcher watches, under its
// own name.
func TestReceive(t *testing.T) {
	w, err := New([]string{"prod"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Receive("prod", report.Report{Server: "web-1"}); err == nil {
		t.Error("took a report that names another server")
	}
	if err := w.Receive("web-1", report.Report{Server: "web-1"}); err == nil {
		t.Error("took a report of a server it does not watch")
	}
	if err := w.Receive("prod", testReport(report.App{Name: "a b"})); err == nil {
		t.Error("took an odd app name")
	}
	if err := w.Receive("prod", testReport(shop)); err != nil {
		t.Fatal(err)
	}
	view := w.View()
	if len(view.Servers) != 1 || view.Servers[0].Name != "prod" || len(view.Servers[0].Apps) != 1 || view.Servers[0].Apps[0].Name != "shop" {
		t.Errorf("view = %+v", view)
	}
}

// What Save hands out, New takes back; Save writes only what changed, and
// tries again after a failed write.
func TestSaveAndNew(t *testing.T) {
	w, err := New([]string{"prod"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var saved []byte
	writes := 0
	write := func(data []byte) error { writes++; saved = data; return nil }
	if err := w.Save(write); err != nil || writes != 0 {
		t.Fatalf("saved nothing new: %d writes, %v", writes, err)
	}
	if err := w.Receive("prod", testReport(shop)); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(func([]byte) error { return os.ErrPermission }); err == nil {
		t.Fatal("a failed write is no error")
	}
	if err := w.Save(write); err != nil || writes != 1 {
		t.Fatalf("after the failed write: %d writes, %v", writes, err)
	}
	if err := w.Save(write); err != nil || writes != 1 {
		t.Fatalf("saved twice: %d writes, %v", writes, err)
	}
	again, err := New([]string{"prod"}, nil, saved)
	if err != nil || again.state.Servers["prod"] == nil || again.state.Domains["shop.example.com"] == nil {
		t.Fatalf("New from saved: %v, %+v", err, again.state)
	}
	if _, err := New(nil, nil, []byte("{")); err == nil {
		t.Error("took a state that is not JSON")
	}
}

// moin cloud sends its own kind of alert, like mail, through Send.
func TestOwnSend(t *testing.T) {
	mail := alert.Destination{Kind: "mail", URL: "mailto:ada@example.com"}
	w, err := newAt([]string{"prod"}, []alert.Destination{mail}, nil, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var sent []alert.Message
	w.Send = func(_ context.Context, to alert.Destination, m alert.Message) error {
		if to != mail {
			t.Errorf("sent to %+v", to)
		}
		sent = append(sent, m)
		return nil
	}
	w.state.receive("prod", testReport(), time.Now().Add(-10*time.Minute))
	w.Tick(t.Context())
	if len(sent) != 1 || sent[0].Title != "prod stopped reporting" || len(w.state.Unsent) != 0 {
		t.Errorf("sent = %+v, unsent = %d", sent, len(w.state.Unsent))
	}
}

// A visitor runs no more visits at once than it may.
func TestVisitorAtOnce(t *testing.T) {
	v := NewVisitor(2)
	v.slots <- struct{}{}
	v.slots <- struct{}{}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if got := v.Visit(ctx, "shop.example.com"); got != "stopped" {
		t.Errorf("a visit with no free slot = %q, want it to wait", got)
	}
}

// SetServers watches a new server and forgets one that is gone.
func TestSetServers(t *testing.T) {
	w, err := New([]string{"prod"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Receive("prod", testReport(shop)); err != nil {
		t.Fatal(err)
	}
	w.Connect = func(server string) string { return "moin watcher connect " + server }
	w.SetServers([]string{"web-1"})
	if len(w.state.Servers) != 0 || len(w.state.Domains) != 0 {
		t.Errorf("prod is still known: %+v", w.state)
	}
	view := w.View()
	if len(view.NeedsYou) != 1 || !strings.Contains(view.NeedsYou[0], "moin watcher connect web-1") {
		t.Errorf("needs you = %v", view.NeedsYou)
	}
	if err := w.Receive("prod", testReport(shop)); err == nil {
		t.Error("took a report of a server it no longer watches")
	}
}

// An alert about a server goes where its last report says and to the
// watcher's own destinations, each once. A server whose report names
// none, like one with an older moin, has the watcher's own only.
func TestReportAlerts(t *testing.T) {
	own := alert.Destination{Kind: "webhook", URL: "https://own.example.com/hook"}
	chat := alert.Destination{Kind: "telegram", Token: "123:test", ChatID: "42"}
	w, err := newAt([]string{"prod", "web-1"}, []alert.Destination{own}, nil, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	sent := map[string][]alert.Destination{}
	w.Send = func(_ context.Context, to alert.Destination, m alert.Message) error {
		mu.Lock()
		defer mu.Unlock()
		sent[m.Server] = append(sent[m.Server], to)
		return nil
	}
	w.SendReported = w.Send
	withAlerts := testReport()
	withAlerts.Alerts = []report.Alert{chat, own}
	if err := w.Receive("prod", withAlerts); err != nil {
		t.Fatal(err)
	}
	old := testReport()
	old.Server = "web-1"
	if err := w.Receive("web-1", old); err != nil {
		t.Fatal(err)
	}
	for _, srv := range w.state.Servers {
		srv.Received = time.Now().Add(-10 * time.Minute)
	}
	w.Tick(t.Context())
	if got := sent["prod"]; len(got) != 2 || !slices.Contains(got, chat) || !slices.Contains(got, own) {
		t.Errorf("prod alerted to %+v", sent["prod"])
	}
	if !slices.Equal(sent["web-1"], []alert.Destination{own}) {
		t.Errorf("web-1 alerted to %+v", sent["web-1"])
	}

	// An unsent alert to a destination the server removed is dropped.
	w.state.Unsent = []unsent{{To: chat}, {To: own}}
	if err := w.Receive("prod", testReport()); err != nil {
		t.Fatal(err)
	}
	if len(w.state.Unsent) != 1 || w.state.Unsent[0].To != own {
		t.Errorf("unsent = %+v", w.state.Unsent)
	}

	// A destination the watcher cannot send to is left out.
	unknown := testReport()
	unknown.Alerts = []report.Alert{{Kind: "pager", URL: "https://pager.example.com"}, chat}
	if err := w.Receive("prod", unknown); err != nil {
		t.Fatal(err)
	}
	if got := w.state.Servers["prod"].Report.Alerts; !slices.Equal(got, []report.Alert{chat}) {
		t.Errorf("kept the alerts %+v", got)
	}

	many := testReport()
	many.Alerts = make([]report.Alert, maxAlerts+1)
	if err := w.Receive("prod", many); err == nil {
		t.Error("took a report with too many alerts")
	}
}

// A destination from a report goes out the public door only: it never
// reaches an address on the watcher's own network. The same address in
// the watcher's own list, written by its owner, still gets the alert.
func TestReportedGoesPublicOnly(t *testing.T) {
	calls := 0
	inside := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer inside.Close()
	hook := alert.Destination{Kind: "webhook", URL: inside.URL + "/hook"}
	down := unsent{To: hook, Message: alert.Message{Server: "prod", Title: "prod stopped reporting"}}

	reported, err := New([]string{"prod"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	reported.state.Unsent = []unsent{down}
	reported.Tick(t.Context())
	if calls != 0 || len(reported.state.Unsent) != 1 {
		t.Errorf("a reported destination inside: %d calls, %d unsent", calls, len(reported.state.Unsent))
	}

	own, err := New([]string{"prod"}, []alert.Destination{hook}, nil)
	if err != nil {
		t.Fatal(err)
	}
	own.state.Unsent = []unsent{down}
	own.Tick(t.Context())
	if calls != 1 || len(own.state.Unsent) != 0 {
		t.Errorf("the watcher's own destination: %d calls, %d unsent", calls, len(own.state.Unsent))
	}

	// A report cannot name it at all: without https it is left out.
	named := testReport()
	named.Alerts = []report.Alert{hook}
	if err := reported.Receive("prod", named); err != nil {
		t.Fatal(err)
	}
	if got := reported.state.Servers["prod"].Report.Alerts; len(got) != 0 {
		t.Errorf("kept the alerts %+v", got)
	}
}

// Every destination gets its alerts at the same time: a slow one does not
// hold up the others. One destination gets its alerts in order, and none
// after the first that failed.
func TestSendAllAtOnce(t *testing.T) {
	slow := alert.Destination{Kind: "webhook", URL: "https://slow.example.com/hook"}
	fast := alert.Destination{Kind: "webhook", URL: "https://fast.example.com/hook"}
	dead := alert.Destination{Kind: "webhook", URL: "https://dead.example.com/hook"}
	w, err := New([]string{"prod"}, []alert.Destination{slow, fast, dead}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fastGotIt := make(chan struct{})
	var mu sync.Mutex
	var tried []string
	w.Send = func(_ context.Context, to alert.Destination, m alert.Message) error {
		mu.Lock()
		tried = append(tried, to.URL+" "+m.Title)
		mu.Unlock()
		switch to {
		case slow:
			select {
			case <-fastGotIt:
			case <-time.After(5 * time.Second):
				t.Error("the slow destination held up the fast one")
			}
		case fast:
			close(fastGotIt)
		case dead:
			return errors.New("no answer")
		}
		return nil
	}
	first, second := alert.Message{Title: "first"}, alert.Message{Title: "second"}
	failed := w.sendAll(t.Context(), []unsent{{To: slow, Message: first}, {To: dead, Message: first}, {To: fast, Message: first}, {To: dead, Message: second}})
	if !slices.Equal(failed, []unsent{{To: dead, Message: first}, {To: dead, Message: second}}) {
		t.Errorf("failed = %+v", failed)
	}
	if len(tried) != 3 || slices.Contains(tried, dead.URL+" second") {
		t.Errorf("tried = %v", tried)
	}
}
