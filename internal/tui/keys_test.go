package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/client"
)

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func update(m model, msg tea.Msg) (model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func TestEscGoesBackOneLevelAndQQuitsOnlyTheDashboard(t *testing.T) {
	m := fixture(120, 40)
	m = press(m, "enter") // the first app
	if m.cur.v != vApp || m.cur.app != "shop" || m.cur.tab != tDeploys {
		t.Fatalf("enter on the dashboard opens %+v, want shop deploys", m.cur)
	}
	m = press(m, "l")
	if m.cur.tab != tLogs || len(m.stack) != 1 {
		t.Fatalf("a tab key switches in place: %+v, stack %d", m.cur, len(m.stack))
	}
	m = press(m, "space", "s") // the servers, from the menu
	if m.cur.v != vServers || len(m.stack) != 2 {
		t.Fatalf("space s opens the servers on top: %+v, stack %d", m.cur, len(m.stack))
	}
	m = press(m, "esc")
	if m.cur.v != vApp || m.cur.tab != tLogs {
		t.Fatalf("esc goes back to shop logs, got %+v", m.cur)
	}
	m, cmd := update(m, keyMsg("q"))
	if quits(cmd) || m.cur.v != vDash {
		t.Fatalf("q in an app goes back, got %+v quit=%v", m.cur, quits(cmd))
	}
	m = press(m, "esc")
	if m.cur.v != vDash {
		t.Fatal("esc on the dashboard stays")
	}
	_, cmd = update(m, keyMsg("q"))
	if !quits(cmd) {
		t.Error("q on the dashboard quits")
	}
	_, cmd = update(press(m, "enter"), keyMsg("ctrl+c"))
	if !quits(cmd) {
		t.Error("ctrl+c quits from anywhere")
	}
}

func TestEscClosesALayerBeforeItGoesBack(t *testing.T) {
	m := press(fixture(120, 40), "enter", "space")
	if !m.menuOn {
		t.Fatal("space opens the menu")
	}
	m = press(m, "esc")
	if m.menuOn || m.cur.v != vApp {
		t.Fatalf("esc closes the menu and stays in the app: menu %v, %+v", m.menuOn, m.cur)
	}
	m = press(m, "ctrl+k", "x", "esc")
	if m.pick != nil || m.cur.v != vApp {
		t.Fatal("esc closes the jump list first")
	}
	m = press(m, "?")
	if !m.help {
		t.Fatal("? shows the keys")
	}
	m = press(m, "esc")
	if m.help || m.cur.v != vApp {
		t.Fatal("esc closes the keys first")
	}
}

func TestDashboardTabMovesBetweenPanels(t *testing.T) {
	m := fixture(120, 40)
	m = press(m, "down", "down")
	if m.sel["apps"] != 2 {
		t.Fatalf("down moves in the apps, at %d", m.sel["apps"])
	}
	m = press(m, "tab", "down")
	if m.focus != fServers || m.sel["servers"] != 1 {
		t.Fatalf("tab moves to the servers: focus %d at %d", m.focus, m.sel["servers"])
	}
	m = press(m, "enter")
	if m.cur.v != vServer || m.cur.server != "edge-02" {
		t.Fatalf("enter opens the selected server, got %+v", m.cur)
	}
	m = press(m, "esc", "tab", "enter")
	if m.cur.v != vApp || m.cur.app != "api" {
		t.Fatalf("enter on the first issue opens its app, got %+v", m.cur)
	}
	m = press(m, "esc", "tab")
	if m.focus != fApps {
		t.Fatalf("tab goes round to the apps, focus %d", m.focus)
	}
	m = press(m, "up", "up", "up", "up", "up")
	if m.sel["apps"] != 0 {
		t.Fatal("the cursor stops at the first row")
	}
	m = press(m, "G")
	if m.sel["apps"] != len(m.apps)-1 {
		t.Fatal("G goes to the last row")
	}
}

func TestWhichKeyNamesEveryActionAndRunsIt(t *testing.T) {
	m := press(fixture(120, 40), "space")
	_, groups := m.menuGroups()
	if len(groups) != 1 || groups[0].name != "Go to" {
		t.Fatalf("on the dashboard the menu has Go to only, got %d groups", len(groups))
	}
	m = press(m, "a")
	title, groups := m.menuGroups()
	if title != "apps" || len(groups[0].items) != len(m.apps) {
		t.Fatalf("space a lists the apps, got %q with %d", title, len(groups[0].items))
	}
	keys := map[string]bool{}
	for _, it := range groups[0].items {
		if keys[it.key] {
			t.Errorf("key %q twice in the apps menu", it.key)
		}
		keys[it.key] = true
	}
	m = press(m, "backspace")
	if len(m.menu) != 0 || !m.menuOn {
		t.Fatal("backspace goes up one menu level")
	}
	m = press(m, "a", "b")
	if m.menuOn || m.cur.v != vApp || m.cur.app != "blog" {
		t.Fatalf("space a b opens blog, got %+v", m.cur)
	}
	m = press(m, "space")
	_, groups = m.menuGroups()
	names := []string{}
	for _, g := range groups {
		names = append(names, g.name)
	}
	if strings.Join(names, ",") != "Go to,blog,Do" {
		t.Fatalf("in an app the menu has its views and actions, got %v", names)
	}
	seen := map[string]string{}
	for _, g := range groups {
		for _, it := range g.items {
			if other, ok := seen[it.key]; ok {
				t.Errorf("key %q is %q and %q", it.key, other, it.label)
			}
			seen[it.key] = it.label
		}
	}
	m = press(m, "m")
	if m.cur.tab != tMetrics || m.menuOn {
		t.Fatalf("space m opens the metrics, got %+v", m.cur)
	}
	m = press(m, "space", "R")
	if m.ask == nil || !strings.Contains(m.ask.title, "Redeploy blog") {
		t.Fatal("space R asks before it redeploys")
	}
}

func TestAppKeysDoNotShadowTabKeys(t *testing.T) {
	m := fixture(120, 40)
	for _, a := range m.appActions("shop") {
		for _, tb := range tabs {
			if a.key == string(tb.t) {
				t.Errorf("action %q takes the key of the %s tab", a.label, tb.name)
			}
		}
		switch a.key {
		case "G", "g", "j", "k", "p", "r", "a", "x", "c", "v", "t", "E", "[", "]", "/":
			t.Errorf("action %q takes the key %s a tab uses", a.label, a.key)
		}
	}
}

func TestFuzzyFindsSubsequencesBestFirst(t *testing.T) {
	if _, _, ok := fuzzy("shp", "shop"); !ok {
		t.Error("shp matches shop")
	}
	if _, _, ok := fuzzy("pohs", "shop"); ok {
		t.Error("pohs does not match shop")
	}
	exact, _, _ := fuzzy("shop", "shop")
	loose, _, _ := fuzzy("shop", "s h o p x")
	if exact <= loose {
		t.Errorf("a whole match scores above a scattered one: %d <= %d", exact, loose)
	}
	_, pos, _ := fuzzy("sl", "shop logs")
	if len(pos) != 2 || pos[0] != 0 || pos[1] != 5 {
		t.Errorf("positions of sl in shop logs: %v", pos)
	}
}

func TestPickerFiltersAndJumps(t *testing.T) {
	m := press(fixture(120, 40), "ctrl+k")
	all := m.hits()
	for _, h := range all {
		if h.e.kind != "app" && h.e.kind != "server" && h.e.kind != "view" {
			t.Fatalf("without a query the list shows places only, got %s %q", h.e.kind, h.e.title)
		}
	}
	m = press(m, "b", "l", "o", "g", "space", "l", "o")
	hs := m.hits()
	if len(hs) == 0 || hs[0].e.title != "blog logs" {
		t.Fatalf("blog lo finds blog logs first, got %v", titles(hs))
	}
	m = press(m, "enter")
	if m.pick != nil || m.cur.v != vApp || m.cur.app != "blog" || m.cur.tab != tLogs {
		t.Fatalf("enter jumps to blog logs, got %+v", m.cur)
	}
	m = press(m, "ctrl+k", "e", "d", "g", "e", "down", "up")
	if hs := m.hits(); len(hs) == 0 || hs[0].e.title != "edge-02" || m.pick.at != 0 {
		t.Fatalf("edge finds the server, got %v at %d", titles(hs), m.pick.at)
	}
	m = press(m, "backspace", "backspace", "backspace", "backspace", "r", "e", "d", "e", "p", "l", "o", "y", "space", "a", "p", "i", "enter")
	if m.ask == nil || !strings.Contains(m.ask.title, "Redeploy api") || m.cur.app != "api" {
		t.Fatalf("redeploy api opens api and asks, got %+v %v", m.cur, m.ask)
	}
	m = press(m, "esc")
	if m.ask != nil || m.cur.app != "api" {
		t.Fatal("esc cancels the question and stays")
	}
}

func titles(hs []hit) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.e.title)
	}
	return out
}

func TestConfirmRunsOnlyOnYes(t *testing.T) {
	m := fixture(120, 40).withApp("shop", tDeploys)
	m = press(m, "down", "r")
	if m.ask == nil || !strings.Contains(m.ask.title, "v42") {
		t.Fatalf("r asks to roll back to the picked deploy, got %v", m.ask)
	}
	m2, cmd := update(m, keyMsg("n"))
	if m2.ask != nil || cmd != nil {
		t.Error("n cancels without running")
	}
	_, cmd = update(m, keyMsg("y"))
	if cmd == nil {
		t.Error("y runs the rollback")
	}
}

func TestRestoreAsksForTheName(t *testing.T) {
	m := press(fixture(120, 40).withApp("shop", tBackups), "enter")
	if m.ask == nil || m.ask.name != "shop" {
		t.Fatal("enter on a snapshot asks for the app's name")
	}
	m2, cmd := update(m, keyMsg("enter"))
	if cmd != nil || m2.ask == nil {
		t.Error("enter without the name does nothing")
	}
	m = press(m, "s", "h", "o", "p")
	_, cmd = update(m, keyMsg("enter"))
	if cmd == nil {
		t.Error("enter with the name restores")
	}
}

func TestPointInTimeStaysInTheWindow(t *testing.T) {
	m := press(fixture(120, 40).withApp("shop", tBackups), "t")
	if m.restore == nil || len(m.restore.dbs) != 1 || m.restore.dbs[0].Database != "shop.db" {
		t.Fatalf("t opens the window of the databases that keep one: %+v", m.restore)
	}
	start := m.restore.at
	m = press(m, "left", "left")
	if got := start.Sub(m.restore.at).Hours(); got != 2 {
		t.Errorf("left goes back an hour each, moved %v hours", got)
	}
	for range 40 {
		m = press(m, "H")
	}
	from, _ := m.restore.window(now)
	if !m.restore.at.Equal(from) {
		t.Errorf("it stops at the start of the window, at %v", m.restore.at)
	}
	m = press(m, "G")
	if !m.restore.at.Equal(*m.app.backups.Databases[0].ReplicatedAt) {
		t.Error("G goes to the newest change")
	}
	m = press(m, "enter", "s", "h", "o", "p")
	_, cmd := update(m, keyMsg("enter"))
	if cmd == nil {
		t.Error("the name and enter restore")
	}
}

func TestLogsFilterAndScroll(t *testing.T) {
	m := fixture(120, 40).withApp("shop", tLogs)
	m = press(m, "/", "e", "r", "r")
	if m.logs.filter != "err" || len(m.filteredLogs()) != 1 {
		t.Fatalf("the filter applies while you type: %q gives %d", m.logs.filter, len(m.filteredLogs()))
	}
	m = press(m, "enter", "c")
	if m.logs.filter != "" {
		t.Fatal("c clears the filter")
	}
	m = press(m, "up", "up")
	if m.logs.back != 2 {
		t.Fatalf("up scrolls back, at %d", m.logs.back)
	}
	m = press(m, "G")
	if m.logs.back != 0 {
		t.Fatal("G follows the end again")
	}
	m = press(m, "p")
	if !m.logs.paused {
		t.Fatal("p pauses")
	}
	m, _ = update(m, logsMsg{"shop", []string{"new"}})
	if len(m.app.logs) != 1 {
		t.Fatal("lines of the open app replace the old ones")
	}
	m, _ = update(m, logsMsg{"other", []string{"x", "y"}})
	if len(m.app.logs) != 1 {
		t.Fatal("lines of another app are dropped")
	}
}

func TestEnvSetAndRemove(t *testing.T) {
	m := press(fixture(120, 40).withApp("shop", tEnv), "a", "K", "=", "v")
	if m.prompt == nil || m.prompt.value != "K=v" {
		t.Fatalf("a asks for KEY=value, got %+v", m.prompt)
	}
	m2, cmd := update(m, keyMsg("enter"))
	if cmd == nil || m2.prompt != nil {
		t.Error("enter saves it")
	}
	m = press(fixture(120, 40).withApp("shop", tEnv), "a", "n", "o")
	m, cmd = update(m, keyMsg("enter"))
	if cmd != nil || !m.noticeErr {
		t.Error("a line without = is refused")
	}
	m = press(m, "x")
	if m.ask == nil || !strings.Contains(m.ask.title, "DATABASE_URL") {
		t.Fatalf("x asks before it removes the selected var, got %v", m.ask)
	}
}

func TestStartedDeployOpensItsLog(t *testing.T) {
	m := fixture(120, 40).withApp("shop", tDeploys)
	m, _ = update(m, startedMsg{app: "shop", deploy: client.Deploy{Number: 44, Status: "running"}, title: "redeploy v44"})
	if m.cur.v != vDeploy || m.cur.deploy != 44 || !m.out.running {
		t.Fatalf("a started deploy opens its log, got %+v", m.cur)
	}
	id := m.out.id
	m, _ = update(m, hubMsg{lineMsg{id, "building"}})
	m, _ = update(m, lineMsg{id - 1, "stale"})
	if len(m.out.lines) != 1 || m.out.lines[0] != "building" {
		t.Fatalf("lines of the open deploy come in, others drop: %v", m.out.lines)
	}
	m, _ = update(m, doneMsg{id: id, status: "succeeded", notice: "moin. shop is live, v44"})
	if m.out.running || m.notice != "moin. shop is live, v44" {
		t.Fatal("the end of the deploy says so")
	}
	m = press(m, "esc")
	if m.cur.v != vApp || m.cur.tab != tDeploys {
		t.Fatalf("esc goes back to the deploys, got %+v", m.cur)
	}
}

func TestOpeningAnotherAppDropsTheOldData(t *testing.T) {
	m := fixture(120, 40).withApp("shop", tEnv)
	m, _ = m.openApp("blog", tEnv)
	if m.app.name != "blog" || m.app.env != nil {
		t.Fatal("another app starts empty")
	}
	m, _ = update(m, envMsg{"shop", map[string]string{"A": "1"}})
	if m.app.env != nil {
		t.Fatal("a late answer for shop does not show in blog")
	}
}

func TestWebPagesOpenTheBrowser(t *testing.T) {
	var opened []string
	old := openBrowser
	openBrowser = func(url string) error { opened = append(opened, url); return nil }
	defer func() { openBrowser = old }()
	m := press(fixture(120, 40), "w", "b")
	if len(opened) != 1 || opened[0] != "https://moin.example/app/acme/billing" {
		t.Fatalf("b opens billing, got %v", opened)
	}
	m.ws.selfHosted = true
	for _, p := range m.webPages() {
		if p.name == "Billing" {
			t.Error("a self-hosted control plane has no billing")
		}
	}
	press(fixture(120, 40).withApp("shop", tDeploys), "W")
	if opened[len(opened)-1] != "https://moin.example/app/acme/apps/shop" {
		t.Errorf("W opens the app in the web, got %v", opened)
	}
}

func TestLoginShowsTheCodeThenConnects(t *testing.T) {
	old := openBrowser
	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = old }()
	called := false
	m := newModel(context.Background(), Options{Config: client.Config{URL: "https://moin.example"}, Login: func(ctx context.Context, show func(string, string)) error {
		called = true
		show("WXYZ-0001", "https://moin.example/device")
		return nil
	}})
	m.w, m.h = 100, 30
	if m.auth == nil {
		t.Fatal("without a token the TUI signs in first")
	}
	m.startLogin()
	msg := <-m.hub
	if !called {
		t.Fatal("the login function runs")
	}
	m, _ = update(m, msg)
	if m.auth.code != "WXYZ-0001" || opened != "https://moin.example/device" {
		t.Fatalf("the code shows and the browser opens: %+v %q", m.auth, opened)
	}
	if !strings.Contains(plain(m.render()), "W X Y Z - 0 0 0 1") {
		t.Error("the code is on the screen")
	}
	m, _ = update(m, signedInMsg{err: errors.New("the code expired")})
	if !strings.Contains(plain(m.render()), "the code expired") {
		t.Error("a failed sign-in says why")
	}
	m = press(m, "r")
	if m.auth.err != nil {
		t.Error("r tries again")
	}
	api, _ := client.New(client.Config{URL: "https://moin.example", Token: "pat_x"})
	m, cmd := update(m, signedInMsg{api: api})
	if m.auth != nil || m.api == nil || cmd == nil {
		t.Fatal("a sign-in lands on the dashboard and loads it")
	}
	_, cmd = update(m, keyMsg("esc"))
	if quits(cmd) {
		t.Error("esc on the dashboard does not quit")
	}
}

func TestLoginKeys(t *testing.T) {
	m := fixture(100, 30)
	m.auth, m.login = &auth{}, nil
	if _, cmd := update(m, keyMsg("x")); !quits(cmd) {
		t.Error("without a login function any key quits")
	}
	m.login = fakeLogin
	if _, cmd := update(m, keyMsg("x")); quits(cmd) {
		t.Error("during the sign-in other keys wait")
	}
	if _, cmd := update(m, keyMsg("esc")); !quits(cmd) {
		t.Error("esc quits the sign-in")
	}
}

func TestPasteGoesIntoTheOpenInput(t *testing.T) {
	m := press(fixture(120, 40).withApp("shop", tEnv), "a")
	m, _ = update(m, tea.PasteMsg{Content: "KEY=multi\nline"})
	if m.prompt.value != "KEY=multi line" {
		t.Errorf("paste lands on one line, got %q", m.prompt.value)
	}
}

func TestAssignKeysGivesEveryNameOne(t *testing.T) {
	keys := assignKeys([]string{"shop", "shop-2", "s", "ss", "api"})
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Errorf("key %q twice in %v", k, keys)
		}
		seen[k] = true
	}
	if keys["shop"] != "s" || keys["api"] != "a" {
		t.Errorf("first letters first: %v", keys)
	}
}
