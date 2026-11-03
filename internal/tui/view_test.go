package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/protocol"
	"regexp"
)

// frames are the screens worth checking: every place, prompt and panel.
func frames() map[string]model {
	now := time.Now()
	seen := now.Add(-3 * time.Minute)
	finished := now.Add(-time.Hour + 16*time.Second)
	servers := []client.Server{
		{ID: "1", Name: "dock-1", Online: true, OS: "linux", Arch: "arm64", AgentVersion: "dev", LastSeenAt: &now},
		{ID: "2", Name: "web-2", OS: "linux", Arch: "amd64", AgentVersion: "v3.2.1", LastSeenAt: &seen, Rules: &protocol.Rules{DataOff: true, DeployFrom: []string{"github.com/acme/*"}, NetworkLocked: true}},
		{ID: "3", Name: "tui-1"},
	}
	apps := []client.App{
		{Name: "hello", ServerName: "dock-1", LatestNumber: 42, LatestStatus: "succeeded", LiveNumber: 42},
		{Name: "blog", ServerName: "web-2", LatestNumber: 4, LatestStatus: "failed", LiveNumber: 3},
		{Name: "shop", ServerName: "dock-1", LatestNumber: 7, LatestStatus: "running", LiveNumber: 6},
		{Name: "notes", ServerName: "dock-1"},
	}
	detected := &protocol.Detected{Kind: "compose", Service: "web", Port: 8080, Databases: []protocol.Database{{Path: "/data/app.db", HostPath: "/var/lib/ops/apps/hello/volumes/data/app.db"}}}
	var deploys []client.Deploy
	for number := 43; number > 30; number-- {
		deploy := client.Deploy{Number: number, Status: "succeeded", Detected: detected, CreatedAt: now.Add(-time.Hour), FinishedAt: &finished}
		switch number {
		case 43:
			deploy.Status, deploy.FinishedAt = "running", nil
		case 40:
			deploy.Status, deploy.Error = "failed", "health check /index.html failed: 502 Bad Gateway"
		case 39:
			deploy.FromNumber = 37
		}
		deploys = append(deploys, deploy)
	}
	detail := &client.AppDetail{App: apps[0], Deploys: deploys}
	log := []string{"downloading the source", "detected compose, service web on port 8080", "building", " Image ops-hello-web:43 Building ", "#1 [internal] load local bake definitions", "#1 DONE 0.0s", "", "#2 ERROR: failed to read dockerfile", " Image ops-hello-web:43 Built ", "starting", " Container ops-hello-web-1 Started ", "health check http://172.18.0.2:8080/index.html"}

	base := model{url: "https://ops.goilerplate.com", dir: "/home/me/hello", servers: servers, apps: apps}
	out := map[string]model{}
	add := func(name string, change func(*model)) {
		m := base
		change(&m)
		out[name] = m
	}
	add("servers", func(m *model) {})
	add("servers-loading", func(m *model) { m.servers, m.loading = nil, true })
	add("servers-empty", func(m *model) { m.servers = nil })
	add("servers-add", func(m *model) { m.mode, m.input = modeAdd, "tui-1" })
	add("servers-added", func(m *model) {
		m.added = &client.NewServer{Server: client.Server{Name: "tui-1"}, InstallCommand: "curl -fsSL https://ops.goilerplate.com/install.sh | sh -s -- ins_4k2j9x0c8v7b6n5m4l3k2j1h0g9f8d7s"}
	})
	add("servers-remove", func(m *model) {
		m.selected = 2
		m.confirm = confirmation{question: "Remove " + servers[2].Name + "?", detail: "Its agent is disconnected.", name: servers[2].Name, danger: true}
		m.mode, m.input = modeConfirmName, "web"
	})
	add("servers-offline", func(m *model) { m.offline = errors.New("eof") })
	add("apps", func(m *model) { m.tab = tabApps; m.notice = "v42 of hello is live" })
	add("help", func(m *model) { m.tab, m.mode = tabApps, modeHelp })
	add("command", func(m *model) { m.tab, m.mode, m.input = tabApps, modeCommand, "hook off" })
	add("search", func(m *model) { m.tab, m.mode, m.input = tabApps, modeSearch, "error" })
	add("logs-scrolled", func(m *model) {
		m.tab, m.panelFocus, m.outputTitle, m.outputScroll, m.search, m.panelMatch = tabApps, true, "logs of hello", 5, "line 3", 4
		for index := range 40 {
			m.output = append(m.output, fmt.Sprintf("web-1  | line %d", index))
		}
	})
	add("activity", func(m *model) {
		m.tab = tabActivity
		now := time.Now()
		m.events = []client.Event{
			{Actor: "ada@example.com", Via: "api", Summary: "deployed hello v43 from main at 3f2a9c1", CreatedAt: now},
			{Actor: "GitHub", Via: "github", Summary: "deployed hello v42 from main at 9e1b7d0", CreatedAt: now.Add(-time.Hour)},
			{Actor: "grace@example.com", Via: "web", Summary: "ran UPDATE on orders in hello, 3 rows changed", CreatedAt: now.Add(-26 * time.Hour)},
		}
	})
	add("activity-empty", func(m *model) { m.tab = tabActivity })
	add("apps-empty", func(m *model) { m.tab, m.apps = tabApps, nil })
	add("apps-env", func(m *model) {
		m.tab, m.outputID, m.outputTitle, m.output = tabApps, 1, "env of hello", []string{"GREETING=••••••", "TUI_KEY=••••••"}
		m.mode, m.input = modeEnv, "TUI_KEY=sec"
	})
	add("apps-sql", func(m *model) {
		m.tab, m.outputID, m.outputTitle, m.outputNote = tabApps, 1, "data of hello", "/data/app.db"
		m.output = resultLines([]string{"table", "type", "rows"}, [][]any{{"orders", "table", float64(1)}, {"products", "table", float64(12)}})
		m.mode = modeSQL
	})
	add("detail", func(m *model) { m.tab, m.detail = tabApps, detail })
	add("detail-deploy", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "deploy v43 of hello", log
		m.running, m.runStarted, m.runStep, m.frame = true, now.Add(-8*time.Second), "health check http://172.18.0.2:8080/index.html", 2
	})
	add("detail-deployed", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "deploy v43 of hello", log
		m.outputNote, m.notice = "✓ live · 15s", "v43 of hello is live"
	})
	add("detail-failed", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "deploy v43 of hello", log
		m.err = errors.New("v43 of hello failed: health check /index.html failed: 502")
	})
	add("detail-logs", func(m *model) {
		var lines []string
		for index := range 60 {
			lines = append(lines, fmt.Sprintf("web-1  | 2026-10-31T10:24:%02d.407953672Z hello listening on 8080", index))
		}
		m.tab, m.detail, m.outputID, m.outputTitle, m.outputNote, m.output = tabApps, detail, 1, "logs of hello", "last 200 lines per container", lines
	})
	add("detail-env", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "env of hello", []string{"GREETING=••••••", "TUI_KEY=••••••"}
		m.mode, m.input = modeEnv, "TUI_KEY=sec"
	})
	add("detail-rollback", func(m *model) { m.tab, m.detail, m.mode = tabApps, detail, modeConfirmRollback })
	add("detail-sql", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle = tabApps, detail, 1, "SELECT name, stock FROM products ORDER BY stock DESC LIMIT 2"
		m.output = resultLines([]string{"name", "stock"}, [][]any{{"Cable organizer", float64(61)}, {"Desk lamp", nil}})
		m.mode, m.input = modeSQL, "SELECT * FROM orders WHERE status = 'refunded' AND created_at > '2026-10-01' ORDER BY id DESC"
		m.err = errors.New("near \"FORM\": syntax error")
	})
	add("detail-backups", func(m *model) {
		at := now.Add(-time.Hour)
		list := client.AppBackups{SnapshotBeforeChanges: true, Databases: []protocol.DatabaseBackup{{
			Database: "/data/app.db", Since: &at, ReplicatedAt: &now, VerifiedAt: &at,
			Snapshots: []protocol.Snapshot{{ID: "20261031T143638Z-manual", Reason: "manual", CreatedAt: at}, {ID: "20261031T143526Z-before-v42", Reason: "before-v42", CreatedAt: at}},
		}}}
		output := backupsOutput(1, "hello", list)
		m.tab, m.detail, m.outputID, m.outputTitle, m.outputNote, m.output = tabApps, detail, 1, output.title, output.note, output.lines
		m.mode, m.input = modeBackups, "14:0"
	})
	add("detail-restore", func(m *model) {
		m.tab, m.detail, m.mode = tabApps, detail, modeConfirmName
		m.confirm = confirmation{question: "Restore hello to 20261031T143638Z-manual?", detail: "It replaces the data there now.", name: "hello", danger: true}
	})
	add("detail-sql-write", func(m *model) {
		m.tab, m.detail, m.mode = tabApps, detail, modeConfirmWrite
		m.pendingSQL = "UPDATE settings SET value = 'on' WHERE key = 'maintenance' AND value IS NOT NULL AND key != 'currency'"
	})
	add("detail-pick", func(m *model) { m.tab, m.detail, m.deployPick = tabApps, detail, 40 })
	add("detail-rollback-pick", func(m *model) {
		m.tab, m.detail, m.deployPick, m.mode, m.pendingRollback = tabApps, detail, 40, modeConfirmRollback, 40
	})
	add("detail-env-shown", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "env of hello", []string{"GREETING=hi", "TUI_KEY=secret"}
		m.mode, m.envShown = modeEnv, true
	})
	add("detail-github", func(m *model) { m.tab, m.detail, m.mode, m.input = tabApps, detail, modeGitHub, "link acme/hello main" })
	add("apps-add", func(m *model) {
		*m = m.addAppForm()
		m.tab, m.input = tabApps, "shop"
	})
	stored := settings{loaded: true, storage: &client.BackupStorage{Endpoint: "https://abc.r2.cloudflarestorage.com", Region: "auto", Bucket: "ops-backups", AccessKey: "AKIA123"},
		integrations: client.Integrations{Cloudflare: true, CloudflareAccount: "Acme"},
		registries:   []client.Registry{{Host: "ghcr.io", Username: "acme"}}}
	stored.github.Enabled = true
	var team client.Team
	if err := json.Unmarshal([]byte(`{"workspace":{"name":"Acme","slug":"acme"},"role":"owner","permissions":{"manage_workspace":true,"manage_members":true},
		"members":[{"id":"m1","name":"Ada","email":"ada@example.com","role":"owner"},{"id":"m2","name":"Grace","email":"grace@example.com","role":"member"}],
		"invitations":[{"id":"i1","email":"linus@example.com","role":"admin","expires_at":"`+now.Add(72*time.Hour).Format(time.RFC3339)+`"}]}`), &team); err != nil {
		panic(err)
	}
	add("team", func(m *model) { m.tab, m.team = tabTeam, team })
	add("team-invitation", func(m *model) { m.tab, m.team, m.teamSelected = tabTeam, team, 2 })
	add("team-loading", func(m *model) { m.tab = tabTeam })
	add("team-invite", func(m *model) {
		m.tab, m.team = tabTeam, team
		next, _ := m.teamKey("a")
		*m = next.(model)
		m.input = "linus@example.com"
	})
	add("settings", func(m *model) { m.tab, m.settings = tabSettings, stored })
	add("settings-empty", func(m *model) { m.tab, m.settings = tabSettings, settings{loaded: true} })
	add("settings-bucket", func(m *model) {
		m.tab, m.settings, m.settingSelected = tabSettings, stored, 1
		next, _ := m.editSetting(m.settingRows()[1])
		*m = next.(model)
		m.form.step, m.form.values, m.input = 5, []string{"", "", "", "", ""}, "secret"
	})
	add("settings-remove", func(m *model) {
		m.tab, m.settings, m.settingSelected = tabSettings, stored, 4
		*m = m.removeSetting(m.settingRows()[4])
	})
	return out
}

// press sends keys to a model, ignoring the commands they start.
func press(m model, keys ...string) model {
	for _, key := range keys {
		msg := tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
		switch key {
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "ctrl+u", "ctrl+d", "ctrl+w":
			msg = tea.KeyPressMsg{Code: rune(key[5]), Mod: tea.ModCtrl}
		}
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}

// TestEscNeverQuits: esc closes what is open, only q quits.
func TestEscNeverQuits(t *testing.T) {
	for name, m := range frames() {
		if m.mode != modeList {
			continue
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Errorf("%s: esc quits", name)
			}
		}
		_, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
		if cmd == nil {
			t.Errorf("%s: q does not quit", name)
		} else if _, quit := cmd().(tea.QuitMsg); !quit {
			t.Errorf("%s: q does not quit", name)
		}
	}
}

// TestEmptyAppsKeys: without apps the hint and the footer offer only what
// works there.
// TestSettingsFollowServers: a key stored on another surface comes with
// the servers stream, so the settings tab loads again.
func TestSettingsFollowServers(t *testing.T) {
	m := frames()["settings"]
	if _, cmd := m.Update(serversMsg(m.servers)); cmd == nil {
		t.Error("the settings tab does not reload when the servers change")
	}
}

func TestEmptyAppsKeys(t *testing.T) {
	m := frames()["apps-empty"]
	m.width, m.height = 120, 30
	content := strip(m.View().Content)
	if strings.Contains(content, "Press d") || strings.Contains(content, "   d deploy") || strings.Contains(content, "rollback") {
		t.Errorf("empty apps offers keys that do not work:\n%s", content)
	}
	if !strings.Contains(content, "a create an app") {
		t.Errorf("empty apps does not offer a:\n%s", content)
	}
	other := frames()["apps"]
	other.dir = "/home/me/elsewhere"
	if _, keys := other.prompt(); slices.Contains(keys, [2]string{"d", "deploy elsewhere"}) {
		t.Errorf("d shows for an app that does not exist")
	}
}

// TestDeployPick: the arrows pick a deploy in the open app and r rolls back
// to it; the live one cannot be picked for a rollback.
func TestDeployPick(t *testing.T) {
	m := frames()["detail"]
	m = press(m, "down", "down")
	if m.deployPick != 42 {
		t.Fatalf("pick %d, want 42", m.deployPick)
	}
	m = press(m, "r")
	if m.err == nil || m.mode != modeList {
		t.Errorf("rolling back to the live deploy asks: %v", m.err)
	}
	m = press(m, "down", "r")
	if m.mode != modeConfirmRollback || m.pendingRollback != 41 {
		t.Errorf("mode %d, rollback %d; want a question for v41", m.mode, m.pendingRollback)
	}
	m = press(m, "n", "up", "up", "up")
	if m.deployPick != 0 {
		t.Errorf("up past the newest picks %d", m.deployPick)
	}
	m = press(m, "down", "esc")
	if m.deployPick != 0 || m.detail == nil {
		t.Errorf("esc drops the pick first")
	}
}

// TestForm walks the new-app form: defaults fill empty answers, a needed
// field without one stays.
func TestForm(t *testing.T) {
	m := frames()["apps-empty"]
	m.dir = "/home/me/shop"
	m.servers = m.servers[:1]
	var got []string
	m = press(m, "a")
	if m.mode != modeForm {
		t.Fatalf("a opens no form")
	}
	m.form.submit = func(values []string) tea.Cmd { got = values; return nil }
	m = press(m, "enter", "enter")
	if m.mode != modeList || !slices.Equal(got, []string{"shop", "dock-1"}) {
		t.Errorf("form gave %v, mode %d", got, m.mode)
	}
	m.dir = "/home/me/hello"
	m.apps = frames()["apps"].apps
	m.servers = frames()["apps"].servers
	m = press(m, "a", "enter")
	if m.err == nil || m.form.step != 0 {
		t.Errorf("an empty name with no default goes on")
	}
}

// TestTeamKeys: resend and revoke only on an invitation, and a member
// without rights changes nothing.
func TestTeamKeys(t *testing.T) {
	m := frames()["team-invitation"]
	if _, keys := m.prompt(); !slices.Contains(keys, [2]string{"x", "revoke"}) {
		t.Errorf("an invitation offers no revoke: %v", keys)
	}
	m = press(m, "x")
	if m.mode != modeConfirm || !strings.Contains(m.confirm.question, "linus@example.com") {
		t.Errorf("x asks %q", m.confirm.question)
	}
	m = frames()["team"]
	if _, keys := m.prompt(); slices.Contains(keys, [2]string{"x", "revoke"}) {
		t.Errorf("a member offers revoke")
	}
	m.team.Permissions.ManageMembers, m.team.Permissions.ManageWorkspace, m.team.Role = false, false, "member"
	m = press(m, "a")
	if m.mode != modeList || m.err == nil {
		t.Errorf("a member can invite")
	}
}

// TestListPanel: the app keys open their output panel on the list too,
// moving to another app closes it, and esc closes it.
func TestListPanel(t *testing.T) {
	m := frames()["apps-env"]
	m.width, m.height = 120, 30
	content := strip(m.View().Content)
	if !strings.Contains(content, "env of hello") || !strings.Contains(content, "GREETING=") {
		t.Errorf("the list shows no env:\n%s", content)
	}
	for _, msg := range []outputMsg{
		{id: 2, title: "logs of hello", lines: []string{"web-1  | listening"}},
		{id: 3, title: "data of hello", lines: []string{"TABLE"}},
		{id: 4, title: "backups of hello", lines: []string{"/data/app.db"}},
		{id: 5, title: "domains of hello", lines: []string{"hello.example.com"}},
		{id: 6, title: "GitHub of hello", lines: []string{"acme/hello"}},
	} {
		next, _ := frames()["apps"].Update(msg)
		shown := next.(model)
		shown.width, shown.height = 120, 30
		if content := strip(shown.View().Content); !strings.Contains(content, msg.title) || !strings.Contains(content, msg.lines[0]) {
			t.Errorf("the list shows no %s:\n%s", msg.title, content)
		}
	}
	m = press(m, "esc")
	if m.mode != modeList || m.outputTitle == "" {
		t.Errorf("esc in the prompt closes the panel too")
	}
	if moved := press(m, "down"); moved.outputTitle != "" {
		t.Errorf("another app keeps the panel of hello")
	}
	if closed := press(m, "esc"); closed.outputTitle != "" {
		t.Errorf("esc keeps the panel")
	}
}

// TestCtrlU clears the input of every prompt.
func TestCtrlU(t *testing.T) {
	for _, mode := range []mode{modeAdd, modeEnv, modeImage, modeDomains, modeProtect, modeSQL, modeBackups, modeUptime, modeGitHub} {
		m := frames()["apps"]
		m.mode, m.input = mode, "SELECT 1"
		if m = press(m, "ctrl+u"); m.input != "" || m.mode != mode {
			t.Errorf("mode %d: ctrl+u leaves %q, mode %d", mode, m.input, m.mode)
		}
	}
	m := press(frames()["apps-add"], "ctrl+u")
	if m.input != "" || m.mode != modeForm {
		t.Errorf("form: ctrl+u leaves %q", m.input)
	}
}

func TestPlural(t *testing.T) {
	for count, want := range map[int]string{0: "0 rows", 1: "1 row", 2: "2 rows"} {
		if got := plural(count, "row"); got != want {
			t.Errorf("plural(%d) = %q, want %q", count, got, want)
		}
	}
}

// TestViewFits renders every frame at small and large sizes: it never has
// more lines than the terminal, no line is wider, and the footer shows.
func TestViewFits(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 30}, {60, 12}, {200, 50}, {40, 6}}
	show := os.Getenv("TUI_FRAMES")
	for name, m := range frames() {
		for _, size := range sizes {
			m.width, m.height = size[0], size[1]
			content := m.View().Content
			lines := strings.Split(content, "\n")
			if len(lines) > m.height {
				t.Errorf("%s at %dx%d: %d lines", name, size[0], size[1], len(lines))
			}
			for _, line := range lines {
				if lipgloss.Width(line) > m.width {
					t.Errorf("%s at %dx%d: line %q is %d wide", name, size[0], size[1], strip(line), lipgloss.Width(line))
				}
			}
			_, keys := m.prompt()
			if last := strip(lines[len(lines)-1]); size[1] >= 12 && !strings.Contains(content, keys[len(keys)-1][1]) {
				t.Errorf("%s at %dx%d: footer cut, last line %q", name, size[0], size[1], last)
			}
			if show == name || show == "all" {
				if os.Getenv("TUI_COLOR") == "" {
					content = strip(content)
				}
				fmt.Printf("── %s %dx%d\n%s\n", name, size[0], size[1], content)
			}
		}
	}
}

var escapes = regexp.MustCompile("\x1b\\[[0-9;:]*[A-Za-z]")

func strip(text string) string { return escapes.ReplaceAllString(text, "") }

// TestStopThenStart: once the stop went through, S starts the open app and
// asks no second time to stop it.
func TestStopThenStart(t *testing.T) {
	m := frames()["detail"]
	if m = press(m, "S"); m.mode != modeConfirmStop {
		t.Fatalf("S on a running app asks nothing")
	}
	m = press(m, "y")
	next, _ := m.Update(stoppedMsg("hello"))
	m = next.(model)
	if !m.detail.App.Stopped || !m.apps[0].Stopped {
		t.Fatalf("the app does not know hello stopped")
	}
	if m = press(m, "S"); m.mode == modeConfirmStop {
		t.Errorf("S on a stopped app asks to stop it again")
	}
}

// TestInviteWrongRole: a wrong role keeps the form and the email typed.
func TestInviteWrongRole(t *testing.T) {
	m := press(frames()["team"], "a")
	m.input = "ada@example.com"
	m = press(m, "enter")
	m.input = "boss"
	m = press(m, "enter")
	if m.mode != modeForm || m.form.step != 1 || m.err == nil || !slices.Equal(m.form.values, []string{"ada@example.com"}) {
		t.Errorf("mode %d, step %d, values %v, err %v; want the role asked again", m.mode, m.form.step, m.form.values, m.err)
	}
}

// TestTypedNameConfirms: removing a server runs only once its name is
// typed; another answer changes nothing.
func TestTypedNameConfirms(t *testing.T) {
	base := model{servers: []client.Server{{ID: "s1", Name: "web-1"}}}
	m := press(base, "x")
	if m.mode != modeConfirmName || m.confirm.name != "web-1" {
		t.Fatalf("x: mode %v, name %q", m.mode, m.confirm.name)
	}
	ran := false
	m.confirm.run = func() tea.Msg { ran = true; return nil }
	wrong := press(m, "w", "e", "b", "enter")
	if _, cmd := wrong.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil || wrong.err == nil || wrong.mode != modeList {
		t.Fatalf("a wrong name: mode %v, err %v", wrong.mode, wrong.err)
	}
	m.input = "web-1"
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || next.(model).mode != modeList {
		t.Fatal("the right name does not run")
	}
	cmd()
	if !ran {
		t.Fatal("the right name ran something else")
	}
}

// A restore target that is neither a snapshot ID nor a time is refused
// before the app's name is asked.
func TestRestoreTargetCheckedFirst(t *testing.T) {
	m := frames()["detail-backups"]
	m.input = "bogus"
	m = press(m, "enter")
	if m.mode != modeBackups || m.err == nil || m.input != "bogus" {
		t.Errorf("bogus: mode %v, err %v, input %q", m.mode, m.err, m.input)
	}
	for _, target := range []string{"20261031T143638Z-manual", "14:02"} {
		m := frames()["detail-backups"]
		m.input = target
		if m = press(m, "enter"); m.mode != modeConfirmName {
			t.Errorf("%s: mode %v, err %v", target, m.mode, m.err)
		}
	}
}

// :hook asks first only when a hook exists to replace; :hook off without
// one says so.
func TestHookKeysWithoutAHook(t *testing.T) {
	m := frames()["apps"]
	if m = command(m, "hook"); m.mode == modeConfirm {
		t.Errorf(":hook without a hook asks to replace one")
	}
	m = frames()["apps"]
	if m = command(m, "hook off"); m.mode == modeConfirm || !strings.Contains(m.notice, "has no deploy hook") {
		t.Errorf(":hook off without a hook: mode %v, notice %q", m.mode, m.notice)
	}
	m = frames()["apps"]
	m.apps = slices.Clone(m.apps)
	m.apps[0].Hook = true
	if m = command(m, "hook"); m.mode != modeConfirm {
		t.Errorf(":hook with a hook does not ask: mode %v", m.mode)
	}
}

// command types a command on the command line and runs it.
func command(m model, line string) model {
	m = press(m, ":")
	m.input = line
	return press(m, "enter")
}

// The new app form asks the name or the server again when it is wrong,
// instead of failing after both.
func TestNewAppFormChecksEachField(t *testing.T) {
	m := press(frames()["apps"], "a")
	m.input = "Bad"
	if m = press(m, "enter"); m.mode != modeForm || m.form.step != 0 || m.err == nil {
		t.Fatalf("bad name: mode %d, step %d, err %v", m.mode, m.form.step, m.err)
	}
	m.input = "fresh"
	if m = press(m, "enter"); m.form.step != 1 {
		t.Fatalf("good name: step %d, err %v", m.form.step, m.err)
	}
	m.input = "ghost"
	if m = press(m, "enter"); m.mode != modeForm || m.form.step != 1 || m.err == nil {
		t.Fatalf("unknown server: mode %d, step %d, err %v", m.mode, m.form.step, m.err)
	}
}

// The invite form asks the email again when it is no address.
func TestInviteWrongEmail(t *testing.T) {
	m := press(frames()["team"], "a")
	m.input = "bad"
	if m = press(m, "enter"); m.mode != modeForm || m.form.step != 0 || m.err == nil {
		t.Errorf("mode %d, step %d, err %v; want the email asked again", m.mode, m.form.step, m.err)
	}
}

// The rename form asks again for a name the control plane refuses, but a
// server named before the rule may keep its name.
func TestRenameServerChecksName(t *testing.T) {
	m := frames()["servers"]
	m.servers = slices.Clone(m.servers)
	m.servers[0].Name = "Old Box"
	m = press(m, "c", "w")
	m.input = "New Box"
	if m = press(m, "enter"); m.mode != modeForm || m.err == nil || m.input != "New Box" {
		t.Fatalf("bad name: mode %d, input %q, err %v", m.mode, m.input, m.err)
	}
	m.input = ""
	m.form.submit = func([]string) tea.Cmd { return nil }
	if m = press(m, "enter"); m.mode != modeList || m.err != nil {
		t.Fatalf("same old name: mode %d, err %v", m.mode, m.err)
	}
}

// A one-shot input keeps the typed text and shows the error when the change
// fails, and closes once it worked.
func TestOneShotInputKeepsTextOnError(t *testing.T) {
	m := press(frames()["detail"], "p")
	if m.mode != modeProtect {
		t.Fatalf("p opens no protect input")
	}
	m.input = "nocolon"
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if m.mode != modeProtect || m.input != "nocolon" || m.err == nil {
		t.Fatalf("after an error: mode %d, input %q, err %v", m.mode, m.input, m.err)
	}
	next, _ = m.Update(closedMsg{mode: modeProtect, msg: noticeMsg("Saved")})
	m = next.(model)
	if m.mode != modeList || m.input != "" || m.notice != "Saved" {
		t.Fatalf("after it worked: mode %d, input %q, notice %q", m.mode, m.input, m.notice)
	}
}
