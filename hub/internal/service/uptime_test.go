package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// fakeSites answers the visits of every domain with the status set for it,
// 200 by default, or never for a domain that hangs, and remembers the
// paths it got.
type fakeSites struct {
	mu      sync.Mutex
	status  map[string]int
	hangs   map[string]bool
	hanging int
	paths   map[string][]string
}

func (f *fakeSites) set(domain string, status int) {
	f.mu.Lock()
	f.status[domain] = status
	f.mu.Unlock()
}

// hang makes a domain answer never: its visits wait until they give up.
func (f *fakeSites) hang(domain string) {
	f.mu.Lock()
	f.hangs[domain] = true
	f.mu.Unlock()
}

func (f *fakeSites) visits(domain string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths[domain]...)
}

// waiting is how many visits wait for a domain that hangs right now.
func (f *fakeSites) waiting() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hanging
}

// visitSites points uptime of apps at a local server.
func visitSites(t *testing.T, apps *AppService) *fakeSites {
	t.Helper()
	sites := &fakeSites{status: map[string]int{}, hangs: map[string]bool{}, paths: map[string][]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		domain, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		sites.mu.Lock()
		sites.paths[domain] = append(sites.paths[domain], "/"+path)
		status, hangs := sites.status[domain], sites.hangs[domain]
		if hangs {
			sites.hanging++
		}
		sites.mu.Unlock()
		if hangs {
			<-r.Context().Done()
			sites.mu.Lock()
			sites.hanging--
			sites.mu.Unlock()
			return
		}
		w.WriteHeader(max(status, http.StatusOK))
	}))
	t.Cleanup(server.Close)
	apps.uptime.client = server.Client()
	apps.uptime.url = func(domain, path string) string { return server.URL + "/" + domain + path }
	return sites
}

// visitAll visits every watched domain once, now, one after the other, as
// each watcher would at its moment, and writes the counts.
func visitAll(apps *AppService) {
	now := apps.now()
	apps.watchServers(now)
	watch, _ := apps.syncDomains(now)
	for key, watched := range watch {
		if watched {
			apps.visitDomain(context.Background(), key)
		}
	}
	apps.flushUptime()
}

// stopClock makes apps see the time clock points at.
func stopClock(apps *AppService, at time.Time) *time.Time {
	clock := at
	apps.now = func() time.Time { return clock }
	return &clock
}

func activityActions(t *testing.T, servers *ServerService) []string {
	t.Helper()
	events, err := servers.activity.ListPage(owner.WorkspaceID, model.ActivityFilter{}, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	actions := []string{}
	for _, event := range events {
		actions = append(actions, event.Action+" "+event.Details()["domain"])
	}
	return actions
}

func hasAction(actions []string, action string) bool {
	for _, have := range actions {
		if have == action {
			return true
		}
	}
	return false
}

// deployBlog deploys the app blog with what the daemon detects.
func deployBlog(t *testing.T, apps *AppService, servers *ServerService, server *model.Server, detected protocol.Detected) *model.Deploy {
	t.Helper()
	deploy, err := deploySource(apps)
	if err != nil {
		t.Fatal(err)
	}
	daemonSays(t, servers, server.ID, protocol.TypeDeployDone, protocol.DeployDone{DeployID: deploy.ID, OK: true, Detected: detected})
	return deploy
}

// Uptime visits every domain that serves something: the canonical one and
// the other services' ones, not a domain that only redirects.
func TestUptimeVisitsEveryDomainThatServes(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	sites := visitSites(t, apps)
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com", "www.blog.example.com"}, "blog.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "api", []string{"api.blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	// Before the first deploy nothing is visited.
	visitAll(apps)
	if len(sites.visits("blog.example.com")) != 0 {
		t.Fatal("an app without a live deploy was visited")
	}
	_, uptime, err := apps.Uptime(owner.WorkspaceID, "blog")
	if err != nil || uptime.State != model.UptimePending || len(uptime.Domains) != 2 {
		t.Fatalf("uptime before a deploy = %+v, %v", uptime, err)
	}
	deployBlog(t, apps, servers, server, protocol.Detected{Kind: "compose", Service: "web", Config: &protocol.Config{Health: "/health"}})

	visitAll(apps)
	if visits := sites.visits("blog.example.com"); len(visits) != 1 || visits[0] != "/health" {
		t.Fatalf("visits = %v, want one to the health path", visits)
	}
	if len(sites.visits("api.blog.example.com")) != 1 || len(sites.visits("www.blog.example.com")) != 0 {
		t.Fatalf("api visits = %v, www visits = %v: want the service's domain, not the redirect", sites.visits("api.blog.example.com"), sites.visits("www.blog.example.com"))
	}
	_, uptime, err = apps.Uptime(owner.WorkspaceID, "blog")
	if err != nil || uptime.State != model.UptimeUp || len(uptime.Domains) != 2 {
		t.Fatalf("uptime = %+v, %v", uptime, err)
	}
	first := uptime.Domains[0]
	if first.Domain != "blog.example.com" || first.Path != "/health" || first.CustomPath || first.StatusCode != 200 || first.VisitedAt == nil || first.Uptime24h == nil || *first.Uptime24h != 100 {
		t.Fatalf("blog.example.com = %+v", first)
	}
	if uptime.Domains[1].Domain != "api.blog.example.com" {
		t.Fatalf("second domain = %+v", uptime.Domains[1])
	}

	// Without a canonical domain www serves too and is visited; with one
	// again, its uptime goes.
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com", "www.blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	visitAll(apps)
	if len(sites.visits("www.blog.example.com")) != 1 {
		t.Fatal("www without a canonical domain was not visited")
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com", "www.blog.example.com"}, "blog.example.com"); err != nil {
		t.Fatal(err)
	}
	visitAll(apps)
	stored, _ := apps.apps.DomainUptimes()
	for _, domain := range stored {
		if domain.Domain == "www.blog.example.com" {
			t.Fatalf("the uptime of a redirect is kept: %+v", stored)
		}
	}
}

// A domain is down only after 3 failed visits in a row, and back up only
// after 3 answers in a row: fewer count in the numbers but change nothing.
func TestUptimeThreeInARow(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	sites := visitSites(t, apps)
	clock := stopClock(apps, time.Date(2026, 11, 3, 12, 0, 30, 0, time.UTC))
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	deployBlog(t, apps, servers, server, protocol.Detected{Kind: "compose", Service: "web"})
	minute := func() {
		*clock = clock.Add(time.Minute)
		visitAll(apps)
	}
	state := func() string {
		_, uptime, _ := apps.Uptime(owner.WorkspaceID, "blog")
		return uptime.Domains[0].State
	}
	visitAll(apps)

	// Two failed visits are a blip.
	sites.set("blog.example.com", http.StatusBadGateway)
	minute()
	minute()
	if state() != model.UptimeUp || hasAction(activityActions(t, servers), "uptime.down blog.example.com") {
		t.Fatalf("two failed visits: state %s, activity %v", state(), activityActions(t, servers))
	}
	// The third is down, since the first failed visit, once.
	minute()
	minute()
	_, uptime, _ := apps.Uptime(owner.WorkspaceID, "blog")
	domain := uptime.Domains[0]
	if domain.State != model.UptimeDown || !domain.Since.Equal(time.Date(2026, 11, 3, 12, 1, 30, 0, time.UTC)) || len(domain.Outages) != 1 || domain.Outages[0].Reason != "HTTP 502" {
		t.Fatalf("down = %+v", domain)
	}
	if strings.Count(strings.Join(activityActions(t, servers), ","), "uptime.down") != 1 {
		t.Fatalf("activity = %v", activityActions(t, servers))
	}
	// Every visit counts: one up, four down.
	if *domain.Uptime24h != 20 {
		t.Fatalf("uptime = %v, want 20", *domain.Uptime24h)
	}

	// Back up at the third answer in a row.
	sites.set("blog.example.com", http.StatusOK)
	minute()
	minute()
	if state() != model.UptimeDown {
		t.Fatalf("two answers: state %s, want down", state())
	}
	minute()
	_, uptime, _ = apps.Uptime(owner.WorkspaceID, "blog")
	if uptime.State != model.UptimeUp || uptime.Domains[0].Outages[0].EndedAt == nil {
		t.Fatalf("back up = %+v", uptime)
	}
	if !hasAction(activityActions(t, servers), "uptime.up blog.example.com") {
		t.Fatal("back up is not in the activity")
	}
}

// uptimeStep is one visit of TestUptimeRule and whether it changes the
// state, which sends an alert.
type uptimeStep struct {
	name    string
	up      bool
	changed bool
}

// The state flips only after 3 visits in a row with the opposite answer,
// down and back up alike: a flapping site changes nothing and sends
// nothing.
func TestUptimeRule(t *testing.T) {
	state := &uptimeState{}
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for _, step := range []uptimeStep{
		{"first visit", true, true},
		{"fails once", false, false},
		{"fails twice", false, false},
		{"fails 3 times in a row", false, true},
		{"still down", false, false},
		{"answers once", true, false},
		{"fails again", false, false},
		{"answers once", true, false},
		{"answers twice", true, false},
		{"answers 3 times in a row", true, true},
		{"flaps", false, false},
		{"flaps back", true, false},
		{"flaps", false, false},
		{"flaps back", true, false},
	} {
		if changed := state.see(uptimeResult{at: at, up: step.up}); changed != step.changed {
			t.Fatalf("%s at %s: changed %v, want %v", step.name, at.Format("15:04"), changed, step.changed)
		}
		at = at.Add(time.Minute)
	}
	if state.stored.State != model.UptimeUp {
		t.Fatalf("state = %s, want up", state.stored.State)
	}
}

func TestUptimeVisitSettings(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	sites := visitSites(t, apps)
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com", "www.blog.example.com"}, "blog.example.com"); err != nil {
		t.Fatal(err)
	}
	deployBlog(t, apps, servers, server, protocol.Detected{Kind: "compose", Service: "web"})

	path := "/up"
	for _, domain := range []string{"other.example.org", "www.blog.example.com"} {
		if _, _, err := apps.SetUptime(owner, "blog", domain, &path, nil); !errors.Is(err, ErrUptimeDomain) {
			t.Fatalf("%s: err = %v", domain, err)
		}
	}
	bad := "no slash"
	if _, _, err := apps.SetUptime(owner, "blog", "blog.example.com", &bad, nil); !errors.Is(err, ErrUptimePath) {
		t.Fatalf("bad path: err = %v", err)
	}
	if _, _, err := apps.SetUptime(other, "blog", "blog.example.com", &path, nil); err == nil {
		t.Fatal("another workspace changed the visit")
	}
	_, domain, err := apps.SetUptime(owner, "blog", "Blog.example.com", &path, nil)
	if err != nil || domain.Path != "/up" || !domain.CustomPath {
		t.Fatalf("set path = %+v, %v", domain, err)
	}
	visitAll(apps)
	if visits := sites.visits("blog.example.com"); len(visits) != 1 || visits[0] != "/up" {
		t.Fatalf("visits = %v", visits)
	}

	paused := true
	_, domain, err = apps.SetUptime(owner, "blog", "blog.example.com", nil, &paused)
	if err != nil || domain.State != model.UptimePaused || !domain.Paused {
		t.Fatalf("pause = %+v, %v", domain, err)
	}
	visitAll(apps)
	if len(sites.visits("blog.example.com")) != 1 {
		t.Fatal("a paused domain was visited")
	}

	// Settings survive a restart of the hub.
	apps.uptime = newUptimeRunner()
	_, uptime, _ := apps.Uptime(owner.WorkspaceID, "blog")
	if !uptime.Domains[0].Paused || uptime.Domains[0].Path != "/up" || uptime.State != model.UptimePaused {
		t.Fatalf("after a restart = %+v", uptime)
	}
	actions := activityActions(t, servers)
	if !hasAction(actions, "uptime.path_set blog.example.com") || !hasAction(actions, "uptime.paused blog.example.com") {
		t.Fatalf("activity = %v", actions)
	}
}

// RunUptime gives each domain its own watcher: a domain that never
// answers does not hold up another, a removed domain's watcher stops, and
// uptime stops with its context and writes its counts.
func TestRunUptimeWatchesEachDomainOnItsOwn(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	sites := visitSites(t, apps)
	apps.uptime.every = 50 * time.Millisecond
	if _, err := apps.Create(owner, "blog", "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com", "dead.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	deploy := deployBlog(t, apps, servers, server, protocol.Detected{Kind: "compose", Service: "web"})
	sites.hang("dead.example.com")
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		apps.RunUptime(ctx)
		close(stopped)
	}()
	waitFor(t, "visits of blog.example.com while dead.example.com hangs", func() bool {
		return len(sites.visits("blog.example.com")) >= 5 && sites.waiting() == 1
	})

	if _, err := apps.SetDomains(owner, "blog", "", []string{"blog.example.com"}, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the watcher of the removed domain to stop", func() bool { return sites.waiting() == 0 })
	visited := len(sites.visits("dead.example.com"))
	time.Sleep(4 * apps.uptime.every)
	if len(sites.visits("dead.example.com")) != visited {
		t.Fatal("a removed domain is still visited")
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("RunUptime did not stop")
	}
	if counts, _ := apps.apps.UptimeCounts(deploy.AppID, "blog.example.com", time.Now().Add(-time.Hour)); counts.Visits < 5 {
		t.Fatalf("counts after the stop = %+v, want the visits written", counts)
	}
}

// waitFor waits a few seconds at most until done says so.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("waited in vain for " + what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerDownAndUpGoToTheActivity(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	visitSites(t, apps)
	disconnect := servers.Connect(server, testHello, "203.0.113.5", func() {}, func(protocol.Message) error { return nil })
	disconnect()

	// Gone for a moment is no outage.
	visitAll(apps)
	if hasAction(activityActions(t, servers), "server.down ") {
		t.Fatal("a server gone for a moment is down")
	}
	stopClock(apps, time.Now().Add(5*time.Minute))
	visitAll(apps)
	visitAll(apps)
	actions := activityActions(t, servers)
	if strings.Count(strings.Join(actions, ","), "server.down") != 1 {
		t.Fatalf("activity = %v", actions)
	}
	servers.Connect(server, testHello, "203.0.113.5", func() {}, func(protocol.Message) error { return nil })
	visitAll(apps)
	if !hasAction(activityActions(t, servers), "server.up ") {
		t.Fatalf("activity = %v", activityActions(t, servers))
	}
}
