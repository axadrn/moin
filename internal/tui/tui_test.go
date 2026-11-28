package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/protocol"
)

// fake is a CLI whose functions answer from memory and note what ran.
type fake struct {
	restarted  []string
	rolledBack []string
	restored   []string
	copied     []string
	opened     []string
	releases   []daemon.Release
}

func (f *fake) cli() CLI {
	return CLI{
		Servers: []string{"prod", "web-1"},
		Status: func(ctx context.Context, server string) (daemon.Status, error) {
			return daemon.Status{}, nil
		},
		Logs: func(ctx context.Context, server, app string, out io.Writer) error {
			fmt.Fprintln(out, "listening on :8080")
			fmt.Fprintln(out, "GET / 200")
			<-ctx.Done()
			return ctx.Err()
		},
		Backups: func(ctx context.Context, server, app string, said io.Writer) ([]protocol.DatabaseBackup, error) {
			checked := time.Now().Add(-2 * time.Hour)
			return []protocol.DatabaseBackup{{Database: "postgres", CheckedAt: &checked, CheckedCopy: "c2", Copies: []protocol.Copy{{ID: "c2", At: checked}, {ID: "c1", At: checked}}}}, nil
		},
		Releases: func(ctx context.Context, server, app string, said io.Writer) ([]daemon.Release, error) {
			return f.releases, nil
		},
		Restart: func(ctx context.Context, server, app string, log io.Writer) (daemon.Result, error) {
			f.restarted = append(f.restarted, server+"/"+app)
			return daemon.Result{App: app, Release: 4}, nil
		},
		Rollback: func(ctx context.Context, server, app string, number int, log io.Writer) (daemon.Result, error) {
			f.rolledBack = append(f.rolledBack, fmt.Sprintf("%s/%s v%d", server, app, number))
			return daemon.Result{App: app, Release: 5, From: number}, nil
		},
		Backup: func(ctx context.Context, server, app string, said io.Writer) ([]daemon.Copied, error) {
			f.copied = append(f.copied, server+"/"+app)
			return []daemon.Copied{{Database: "postgres", Copy: "c3"}}, nil
		},
		Restore: func(ctx context.Context, server, app, database, to string, said io.Writer) (daemon.Restored, error) {
			f.restored = append(f.restored, fmt.Sprintf("%s/%s %s %s", server, app, database, to))
			return daemon.Restored{Database: database, To: to, Undo: "undo-1"}, nil
		},
		Explain: func(err error) string { return err.Error() },
	}
}

// start is the screen after prod answered with shop and blog, and web-1
// did not answer.
func start(t *testing.T, f *fake) *model {
	t.Helper()
	m := newModel(t.Context(), f.cli())
	m.open = func(url string) error {
		f.opened = append(f.opened, url)
		return nil
	}
	_, facts := m.Update(statusMsg{server: "prod", status: daemon.Status{Apps: []daemon.AppStatus{
		{Name: "shop", Release: 3, Running: true, Addresses: []string{"https://shop.example.com"}},
		{Name: "blog", Release: 1},
	}}})
	m.Update(statusMsg{server: "web-1", err: errors.New("cannot reach web-1 over SSH")})
	run(m, facts)
	return m
}

// press sends one key, like "r", "enter" or "down".
func press(m *model, key string) tea.Cmd {
	special := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown}
	msg := tea.KeyPressMsg{Text: key}
	if code, ok := special[key]; ok {
		msg = tea.KeyPressMsg{Code: code}
	} else {
		msg.Code = []rune(key)[0]
	}
	_, cmd := m.Update(msg)
	return cmd
}

// run runs cmd and gives each message it returns to the model, the way
// Bubble Tea does.
func run(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, each := range msg {
			run(m, each)
		}
	default:
		_, next := m.Update(msg)
		run(m, next)
	}
}

func TestAServerThatDoesNotAnswerHidesNoOther(t *testing.T) {
	m := start(t, &fake{})
	if got := m.apps(); len(got) != 2 || got[0] != (appKey{"prod", "shop"}) {
		t.Fatalf("apps = %v, want shop and blog on prod", got)
	}
	if m.selected != (appKey{"prod", "shop"}) {
		t.Errorf("selected = %v, want the first app", m.selected)
	}
	view := m.View().Content
	for _, want := range []string{"shop", "blog", "web-1", "unreachable", "1 server unreachable"} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen does not show %q:\n%s", want, view)
		}
	}
	press(m, "2")
	press(m, "down")
	if view := m.View().Content; !strings.Contains(view, "cannot reach web-1") {
		t.Errorf("the servers tab does not say why web-1 is unreachable:\n%s", view)
	}
}

func TestServersNotAskedTwiceAtOnce(t *testing.T) {
	m := newModel(t.Context(), (&fake{}).cli())
	if m.askAll() == nil {
		t.Fatal("the first ask asks nobody")
	}
	if cmd := m.askAll(); cmd != nil {
		t.Error("a server still answering is asked again")
	}
	m.Update(statusMsg{server: "prod"})
	if m.servers[0].asking || !m.servers[1].asking {
		t.Errorf("asking = %v %v, want prod done and web-1 still asking", m.servers[0].asking, m.servers[1].asking)
	}
}

func TestSelectionMovesAndStaysAtTheEnds(t *testing.T) {
	m := start(t, &fake{})
	press(m, "down")
	press(m, "down")
	if m.selected.app != "blog" {
		t.Errorf("selected %q after two downs, want blog", m.selected.app)
	}
	press(m, "k")
	press(m, "up")
	if m.selected.app != "shop" {
		t.Errorf("selected %q after two ups, want shop", m.selected.app)
	}
}

func TestSelectionSurvivesARefreshWithoutTheApp(t *testing.T) {
	m := start(t, &fake{})
	press(m, "down")
	m.Update(statusMsg{server: "prod", status: daemon.Status{Apps: []daemon.AppStatus{{Name: "shop"}}}})
	if m.selected.app != "shop" {
		t.Errorf("selected %q after blog went away, want shop", m.selected.app)
	}
}

func TestTheSideShowsTheFactsOfTheSelectedApp(t *testing.T) {
	m := start(t, &fake{releases: []daemon.Release{{Number: 3, Live: true, At: time.Now().Add(-4 * time.Minute)}, {Number: 2, Before: true}}})
	view := m.View().Content
	for _, want := range []string{"https://shop.example.com", "deployed 4m ago", "1 older", "checked 2h ago"} {
		if !strings.Contains(view, want) {
			t.Errorf("the side does not show %q:\n%s", want, view)
		}
	}
}

func TestTabsSwitch(t *testing.T) {
	m := start(t, &fake{})
	press(m, "tab")
	if m.tab != serversTab {
		t.Fatalf("tab = %v after tab, want servers", m.tab)
	}
	press(m, "enter")
	if m.tab != appsTab || m.selected != (appKey{"prod", "shop"}) {
		t.Errorf("enter on prod: tab %v, selected %v, want the apps of prod", m.tab, m.selected)
	}
	run(m, press(m, "3"))
	if m.tab != backupsTab || !strings.Contains(m.View().Content, "Checked 2h ago") {
		t.Errorf("3 does not show the backups:\n%s", m.View().Content)
	}
}

func TestRestartAsksFirst(t *testing.T) {
	f := &fake{}
	m := start(t, f)
	press(m, "r")
	if m.question == nil || !strings.Contains(m.View().Content, "Restart shop") {
		t.Fatal("r restarts without asking")
	}
	press(m, "n")
	if m.question != nil || len(f.restarted) > 0 || !strings.Contains(m.message, "Nothing changed") {
		t.Fatalf("n restarted: %v, message %q", f.restarted, m.message)
	}
	press(m, "r")
	cmd := press(m, "y")
	if m.action == nil || !strings.Contains(m.View().Content, "restarting shop") {
		t.Error("nothing shows that the restart runs")
	}
	run(m, cmd)
	if len(f.restarted) != 1 || f.restarted[0] != "prod/shop" {
		t.Errorf("restarted %v, want prod/shop", f.restarted)
	}
	if m.action != nil || !strings.Contains(m.message, "shop runs v4") {
		t.Errorf("action %v, message %q", m.action, m.message)
	}
}

func TestAnActionShowsTheStepItIsAt(t *testing.T) {
	m := start(t, &fake{})
	cmd := m.start(appKey{"prod", "shop"}, "restarting", "restarting shop", func(log io.Writer) doneMsg {
		fmt.Fprintln(log, "health check of web")
		fmt.Fprintln(log, "    docker says hello")
		return doneMsg{done: "shop runs v4"}
	})
	msg := cmd()
	_, next := m.Update(msg)
	if !strings.Contains(m.statusLine(), "health check of web") {
		t.Errorf("status line %q, want the step", m.statusLine())
	}
	run(m, next)
	if m.action != nil || !strings.Contains(m.message, "shop runs v4") {
		t.Errorf("action %v, message %q", m.action, m.message)
	}
}

func TestOneActionAtATime(t *testing.T) {
	m := start(t, &fake{})
	m.action = &action{what: "restarting shop"}
	press(m, "r")
	press(m, "b")
	if m.question != nil || m.picker != nil || m.action.what != "restarting shop" {
		t.Error("a second action started while one runs")
	}
}

func TestRollbackPicksTheReleaseBefore(t *testing.T) {
	f := &fake{releases: []daemon.Release{{Number: 3, Live: true}, {Number: 2, Before: true}, {Number: 1}}}
	m := start(t, f)
	run(m, press(m, "b"))
	if m.picker == nil || m.picker.cursor != 1 {
		t.Fatalf("picker = %+v, want it on v2", m.picker)
	}
	press(m, "down")
	press(m, "enter")
	if m.question == nil || !strings.Contains(m.question.text, "v1") {
		t.Fatalf("question = %+v, want one about v1", m.question)
	}
	run(m, press(m, "y"))
	if len(f.rolledBack) != 1 || f.rolledBack[0] != "prod/shop v1" {
		t.Errorf("rolled back %v, want prod/shop v1", f.rolledBack)
	}
	if !strings.Contains(m.message, "shop runs v1 again, as v5") {
		t.Errorf("message %q", m.message)
	}
}

func TestRollbackWithoutAnOlderRelease(t *testing.T) {
	m := start(t, &fake{releases: []daemon.Release{{Number: 1, Live: true}}})
	run(m, press(m, "b"))
	if m.picker != nil || !strings.Contains(m.message, "no older release") {
		t.Errorf("picker %v, message %q", m.picker, m.message)
	}
}

func TestEscClosesThePicker(t *testing.T) {
	f := &fake{releases: []daemon.Release{{Number: 2, Live: true}, {Number: 1, Before: true}}}
	m := start(t, f)
	run(m, press(m, "b"))
	press(m, "esc")
	if m.picker != nil || m.question != nil {
		t.Error("esc left the picker open")
	}
}

func TestRestorePicksACopyAndAsks(t *testing.T) {
	f := &fake{}
	m := start(t, f)
	run(m, press(m, "3"))
	press(m, "enter")
	if m.picker == nil || m.picker.cursor != 0 {
		t.Fatalf("picker = %+v, want it on the newest copy", m.picker)
	}
	press(m, "down")
	press(m, "enter")
	if m.question == nil || !strings.Contains(m.question.text, "c1") {
		t.Fatalf("question = %+v, want one about c1", m.question)
	}
	run(m, press(m, "y"))
	if len(f.restored) != 1 || f.restored[0] != "prod/shop postgres c1" {
		t.Errorf("restored %v", f.restored)
	}
	if !strings.Contains(m.message, "back at c1") {
		t.Errorf("message %q", m.message)
	}
}

func TestBackupNowAsksFirst(t *testing.T) {
	f := &fake{}
	m := start(t, f)
	press(m, "c")
	run(m, press(m, "y"))
	if len(f.copied) != 1 || !strings.Contains(m.message, "Copied postgres of shop") {
		t.Errorf("copied %v, message %q", f.copied, m.message)
	}
}

func TestEnterShowsTheLogs(t *testing.T) {
	m := start(t, &fake{})
	follow := press(m, "enter")
	if m.app == nil || m.app.key != (appKey{"prod", "shop"}) {
		t.Fatalf("app screen = %+v, want shop", m.app)
	}
	// The first line, then the second, as Bubble Tea would deliver them.
	_, next := m.Update(follow())
	m.Update(next())
	if got := strings.Join(m.app.logs, "|"); got != "listening on :8080|GET / 200" {
		t.Errorf("logs = %q", got)
	}
	if view := m.View().Content; !strings.Contains(view, "GET / 200") || !strings.Contains(view, "following") {
		t.Errorf("the app screen does not show the logs:\n%s", view)
	}
	screen := m.app.screen
	press(m, "esc")
	if m.app != nil {
		t.Fatal("esc did not go back to the list")
	}
	if cmd := m.gotLogLine(logLineMsg{screen: screen, line: "late"}); cmd != nil {
		t.Error("a closed screen keeps reading its logs")
	}
}

func TestOpenAddress(t *testing.T) {
	f := &fake{}
	m := start(t, f)
	run(m, press(m, "o"))
	if len(f.opened) != 1 || f.opened[0] != "https://shop.example.com" {
		t.Errorf("opened %v", f.opened)
	}
	press(m, "down")
	run(m, press(m, "o"))
	if len(f.opened) != 1 || !strings.Contains(m.message, "blog has no address yet") {
		t.Errorf("opened %v, message %q", f.opened, m.message)
	}
}

func TestHelpAndQuit(t *testing.T) {
	m := start(t, &fake{})
	press(m, "?")
	if !m.help || !strings.Contains(m.View().Content, "restart the app") {
		t.Fatal("? shows no help")
	}
	press(m, "x")
	if m.help {
		t.Error("a key did not close the help")
	}
	if cmd := press(m, "q"); cmd == nil {
		t.Error("q does not quit")
	}
}

func TestEveryLineFitsTheScreen(t *testing.T) {
	for _, width := range []int{50, 80, 140} {
		m := start(t, &fake{})
		m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
		for _, key := range []string{"", "2", "3", "?"} {
			if key != "" {
				press(m, key)
			}
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != 20 {
				t.Errorf("width %d, key %q: %d lines, want 20", width, key, len(lines))
			}
			for _, line := range lines {
				if lipgloss.Width(line) > width {
					t.Errorf("width %d, key %q: a line is %d wide: %q", width, key, lipgloss.Width(line), line)
				}
			}
		}
	}
}

func TestTheSelectedAppStaysOnTheScreen(t *testing.T) {
	m := newModel(t.Context(), CLI{Servers: []string{"prod"}})
	m.height = 12
	var apps []daemon.AppStatus
	for i := range 30 {
		apps = append(apps, daemon.AppStatus{Name: fmt.Sprintf("app-%02d", i)})
	}
	m.Update(statusMsg{server: "prod", status: daemon.Status{Apps: apps}})
	for range 25 {
		m.move(1)
	}
	if view := m.View().Content; !strings.Contains(view, "app-25") {
		t.Errorf("the selected app is not on the screen:\n%s", view)
	}
	if lines := strings.Count(m.View().Content, "\n") + 1; lines != m.height {
		t.Errorf("the screen has %d lines, want %d", lines, m.height)
	}
}

func TestWhatNeedsYouIsOnItsServer(t *testing.T) {
	m := newModel(t.Context(), CLI{Servers: []string{"prod"}})
	m.width = 120
	m.Update(statusMsg{server: "prod", status: daemon.Status{
		PublicSSH: true,
		Needs:     []daemon.Need{{Text: "shop: postgres has no backup yet.", Command: "moin bucket set prod"}},
		Apps:      []daemon.AppStatus{{Name: "shop", Release: 1, Running: true}},
	}})
	if !strings.Contains(m.View().Content, "2 need you") {
		t.Errorf("the header does not say what needs you:\n%s", m.View().Content)
	}
	press(m, "2")
	view := m.View().Content
	for _, want := range []string{"port 22 is open to the internet", "moin secure prod", "shop: postgres has no backup yet.", "moin bucket set prod"} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen does not show %q:\n%s", want, view)
		}
	}
}

func TestNoAppsSaysHowToDeploy(t *testing.T) {
	m := newModel(t.Context(), CLI{Servers: []string{"prod"}})
	m.Update(statusMsg{server: "prod", status: daemon.Status{}})
	if view := m.View().Content; !strings.Contains(view, "No apps yet") || !strings.Contains(view, "moin deploy prod") {
		t.Errorf("an empty server does not say how to deploy:\n%s", view)
	}
}
