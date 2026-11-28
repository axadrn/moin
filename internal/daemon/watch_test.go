package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/report"
	"github.com/axadrn/moin/protocol"
)

// journal catches what the daemon writes to its log during the test.
func journal(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &out
}

// webhook is a destination for alerts that keeps what it got.
type webhook struct {
	mu   sync.Mutex
	got  []alert.Message
	fail bool
}

func (h *webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fail {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	var message alert.Message
	_ = json.NewDecoder(r.Body).Decode(&message)
	h.got = append(h.got, message)
}

func (h *webhook) titles() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	titles := []string{}
	for _, message := range h.got {
		titles = append(titles, message.Title)
	}
	return strings.Join(titles, " | ")
}

// A problem alerts once when it starts and once when it is over, also
// across a restart of the daemon. An alert nobody took goes out again.
func TestAlertOnceAndWhenOver(t *testing.T) {
	journal(t)
	hook := &webhook{}
	server := httptest.NewServer(hook)
	defer server.Close()
	config := Config{Name: "prod", Alerts: []alert.Destination{{Kind: "webhook", URL: server.URL}}}
	dir := t.TempDir()
	problem := map[string]alert.Message{"disk": {Server: "prod", Title: "the disk is 95% full"}}

	w := newWatch(newApps(dir))
	hook.fail = true
	w.alert(context.Background(), config, problem)
	hook.fail = false
	w.alert(context.Background(), config, problem)
	w.alert(context.Background(), config, problem)
	w.save()
	if got := hook.titles(); got != "the disk is 95% full" {
		t.Fatalf("alerts = %q", got)
	}
	// A new daemon knows the problem is open.
	w = newWatch(newApps(dir))
	w.alert(context.Background(), config, problem)
	w.alert(context.Background(), config, nil)
	w.alert(context.Background(), config, nil)
	if got := hook.titles(); got != "the disk is 95% full | over: the disk is 95% full" {
		t.Fatalf("alerts = %q", got)
	}
	if !hook.got[1].OK || hook.got[0].OK {
		t.Fatalf("ok flags: %+v", hook.got)
	}
	if hook.got[0].Event != "disk" || hook.got[1].Event != "disk" {
		t.Fatalf("events: %+v", hook.got)
	}
}

// An app stopped on purpose is in the report as stopped, crashes no
// more, and a deploy that starts it ends that.
func TestStoppedOnPurpose(t *testing.T) {
	a := newApps(t.TempDir())
	os.MkdirAll(a.releaseDir("shop", 1), 0o700)
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	w := newWatch(a)
	if w.reportApp("shop", appContainers{}, nil).Stopped {
		t.Fatal("an app that stopped by itself is stopped on purpose")
	}
	os.WriteFile(a.stoppedFile("shop"), nil, 0o600)
	if app := w.reportApp("shop", appContainers{}, nil); !app.Stopped || app.Running {
		t.Fatalf("app = %+v", app)
	}
	w.open["crash/shop"] = alert.Message{Title: "shop keeps crashing"}
	if problems := w.problems(Config{Name: "prod"}, map[string]appContainers{"shop": {restarts: 9}}, nil); len(problems) != 0 {
		t.Fatalf("an app stopped on purpose has problems: %v", problems)
	}
}

// The problems of a server: a backup that fails, a copy that did not
// restore, an app that keeps crashing, and watchers nobody reaches.
func TestProblems(t *testing.T) {
	w := newWatch(newApps(t.TempDir()))
	config := Config{Name: "prod", Bucket: &Bucket{Bucket: "b"}}
	statuses := map[string][]protocol.DatabaseBackup{"shop": {
		{Database: "/data/app.db", Error: "the bucket said no"},
		{Database: "postgres-db", CheckError: "the copy did not restore"},
		{Database: "/data/fine.db"},
	}}
	w.missed = watcherMisses
	problems := w.problems(config, map[string]appContainers{"shop": {running: true}}, statuses)
	for _, key := range []string{"backup/shop//data/app.db", "check/shop/postgres-db", "watchers"} {
		if _, ok := problems[key]; !ok {
			t.Errorf("no problem %s in %v", key, problems)
		}
	}
	if len(problems) > 4 {
		t.Errorf("problems = %v", problems)
	}
	// Without a bucket a missing backup is no alert.
	problems = w.problems(Config{Name: "prod"}, nil, map[string][]protocol.DatabaseBackup{"shop": {{Database: "/data/app.db", Error: errNoBucket.Error()}}})
	if _, ok := problems["backup/shop//data/app.db"]; ok {
		t.Error("a server without a bucket alerts")
	}
}

// A bucket that is away is one problem for the server, however many
// databases it has, and a check nobody ran for two days is one too.
func TestBucketAndLateCheckProblems(t *testing.T) {
	w := newWatch(newApps(t.TempDir()))
	config := Config{Name: "prod", Bucket: &Bucket{Bucket: "b"}}
	longAgo, today := time.Now().Add(-3*day), time.Now().Add(-time.Hour)
	statuses := map[string][]protocol.DatabaseBackup{
		"shop": {{Database: "/data/app.db", Error: "copy /data/app.db: the bucket: connection refused", BucketDown: true}, {Database: "postgres-db", Error: "copy postgres-db: the bucket: connection refused", BucketDown: true, CheckedAt: &longAgo}},
		"blog": {{Database: "/data/blog.db", Error: "copy /data/blog.db: the bucket: connection refused", BucketDown: true, CheckedAt: &today}},
	}
	problems := w.problems(config, nil, statuses)
	if len(problems) != 2 || problems["bucket"].Title != "the bucket takes no backups" || !strings.Contains(problems["bucket"].Text, "connection refused") {
		t.Fatalf("problems = %v", problems)
	}
	if late := problems["check-late/shop/postgres-db"]; !strings.Contains(late.Title, "the restore check of postgres-db is late") {
		t.Fatalf("problems = %v", problems)
	}
}

// What the watch counts is written down: a daemon that starts again
// still knows the app that keeps crashing and the watchers it misses, and
// calls neither over.
func TestWatchCountsOutliveARestart(t *testing.T) {
	dir := t.TempDir()
	w := newWatch(newApps(dir))
	for _, restarts := range []int{0, 1, 2, 3} {
		w.crashing("shop", restarts)
	}
	w.missed = watcherMisses
	w.open["crash/shop"] = alert.Message{Title: "shop keeps crashing"}
	w.save()
	again := newWatch(newApps(dir))
	problems := again.problems(Config{Name: "prod"}, map[string]appContainers{"shop": {running: true, restarts: 4}}, nil)
	if _, ok := problems["crash/shop"]; !ok {
		t.Errorf("the crashing app is over after a restart: %v", problems)
	}
	if _, ok := problems["watchers"]; !ok {
		t.Errorf("the missed watchers are over after a restart: %v", problems)
	}
	// Nothing changed, nothing is written.
	saved := again.saved
	again.save()
	info, _ := os.Stat(again.stateFile())
	again.save()
	if after, _ := os.Stat(again.stateFile()); saved != nil || !after.ModTime().Equal(info.ModTime()) {
		t.Error("the state is written again without a change")
	}
}

// An app keeps crashing after three restarts in ten looks, and is fine
// again after ten looks without one.
func TestCrashing(t *testing.T) {
	w := newWatch(newApps(t.TempDir()))
	for restarts, want := range []bool{false, false, false, true} {
		if got := w.crashing("shop", restarts); got != want {
			t.Fatalf("after %d restarts: crashing = %v", restarts, got)
		}
	}
	w.open["crash/shop"] = alert.Message{}
	for look := 1; look < crashWindow-1; look++ {
		if !w.crashing("shop", 3) {
			t.Fatalf("fine again after %d quiet looks", look)
		}
	}
	if w.crashing("shop", 3) {
		t.Fatal("still crashing after a quiet window")
	}
	// One restart now and then is no crash loop.
	if w.crashing("blog", 5) || w.crashing("blog", 6) {
		t.Fatal("one restart is a crash loop")
	}
}

// The disk alerts from 90% on and is fine again under 85%.
func TestDiskFull(t *testing.T) {
	w := newWatch(newApps(t.TempDir()))
	if w.diskFull(89) || !w.diskFull(90) {
		t.Fatal("the disk is full from 90% on")
	}
	w.open["disk"] = alert.Message{}
	if !w.diskFull(86) || w.diskFull(84) {
		t.Fatal("the disk is fine again under 85%")
	}
}

// Every watcher gets a report with the apps, their domains and how their
// databases are backed up. When none takes it, the daemon counts.
func TestReport(t *testing.T) {
	journal(t)
	var got report.Report
	var token string
	watcher := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer watcher.Close()
	a := newApps(t.TempDir())
	release := a.releaseDir("shop", 2)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Domains: []string{"shop.example.com"}, Instant: "shop.203-0-113-7.sslip.io"})
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("2"), 0o600)
	copied := time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)
	statuses := map[string][]protocol.DatabaseBackup{"shop": {{Database: "/data/app.db", CopiedAt: &copied, CheckedAt: &copied, CheckedCopy: "20261127T140000Z-000001"}}}

	w := newWatch(a)
	chat := alert.Destination{Kind: "telegram", Token: "123:test", ChatID: "42"}
	config := Config{Name: "prod", Watchers: []Watcher{{URL: watcher.URL, Token: "t1"}}, Alerts: []alert.Destination{chat}}
	problems := map[string]alert.Message{
		"disk":                     {Title: "the disk is 93% full"},
		"crash/shop":               {Title: "shop keeps crashing"},
		"backup/shop//data/app.db": {Title: "shop: the backup of /data/app.db fails"},
		"watchers":                 {Title: "no watcher is reachable"},
	}
	w.report(context.Background(), config, map[string]appContainers{"shop": {running: true}}, statuses, problems)
	if token != "Bearer t1" || got.Server != "prod" || len(got.Apps) != 1 || w.missed != 0 {
		t.Fatalf("report = %+v, token %q, missed %d", got, token, w.missed)
	}
	if len(got.Alerts) != 1 || got.Alerts[0] != chat {
		t.Fatalf("the report's alerts = %+v", got.Alerts)
	}
	// The server's own problems, without what a database's line says.
	if strings.Join(got.Problems, " | ") != "shop keeps crashing | the disk is 93% full" {
		t.Fatalf("the report's problems = %q", got.Problems)
	}
	app := got.Apps[0]
	if app.Name != "shop" || app.Release != 2 || !app.Running || strings.Join(app.Domains, ",") != "shop.example.com,shop.203-0-113-7.sslip.io" {
		t.Fatalf("app = %+v", app)
	}
	database := app.Databases[0]
	if database.Kind != "sqlite" || !database.LastCopy.Equal(copied) || !database.CheckOK || !database.CheckedCopy.Equal(copied) {
		t.Fatalf("database = %+v", database)
	}
	watcher.Close()
	w.report(context.Background(), config, nil, nil, nil)
	w.report(context.Background(), config, nil, nil, nil)
	if w.missed != 2 {
		t.Fatalf("missed = %d", w.missed)
	}
}

// Behind a Cloudflare Tunnel ports 80 and 443 are closed, so the instant
// address answers nobody. The report leaves it out, or a watcher visits
// it and says the app is down. The status leaves it out too.
func TestTunnelHidesTheInstantAddress(t *testing.T) {
	a := newApps(t.TempDir())
	os.MkdirAll(a.releaseDir("shop", 1), 0o700)
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	saveJSON(filepath.Join(a.releaseDir("shop", 1), "detected.json"), protocol.Detected{Domains: []string{"shop.example.com"}, Instant: "shop.203-0-113-7.sslip.io"})
	w := newWatch(a)
	a.proxyMode = func() string { return "direct" }
	if got := strings.Join(w.reportApp("shop", appContainers{}, nil).Domains, " "); got != "shop.example.com shop.203-0-113-7.sslip.io" {
		t.Fatalf("direct: domains = %q", got)
	}
	a.proxyMode = func() string { return "tunnel" }
	if got := strings.Join(w.reportApp("shop", appContainers{}, nil).Domains, " "); got != "shop.example.com" {
		t.Fatalf("tunnel: domains = %q", got)
	}
	detected, _ := a.live("shop")
	if got := strings.Join(a.addresses(detected), " "); got != "https://shop.example.com" {
		t.Fatalf("tunnel: addresses = %q", got)
	}
}
