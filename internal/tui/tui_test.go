package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/protocol"
)

// fake is a CLI whose functions answer from memory and note what ran.
type fake struct {
	restarted  []string
	rolledBack []string
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
			return []protocol.DatabaseBackup{{Database: "postgres"}}, nil
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
	m.Update(statusMsg{server: "prod", status: daemon.Status{Apps: []daemon.AppStatus{
		{Name: "shop", Release: 3, Running: true, Addresses: []string{"https://shop.example.com"}},
		{Name: "blog", Release: 1},
	}}})
	m.Update(statusMsg{server: "web-1", err: errors.New("cannot reach web-1 over SSH")})
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
	for _, want := range []string{"shop", "blog", "web-1", "unreachable", "cannot reach web-1"} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen does not show %q:\n%s", want, view)
		}
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

func TestRestartAsksFirst(t *testing.T) {
	f := &fake{}
	m := start(t, f)
	press(m, "r")
	if m.question == nil {
		t.Fatal("r restarts without asking")
	}
	press(m, "n")
	if m.question != nil || len(f.restarted) > 0 || !strings.Contains(m.message, "Nothing changed") {
		t.Fatalf("n restarted: %v, message %q", f.restarted, m.message)
	}
	press(m, "r")
	cmd := press(m, "y")
	if m.busy == "" {
		t.Error("nothing shows that the restart runs")
	}
	run(m, cmd)
	if len(f.restarted) != 1 || f.restarted[0] != "prod/shop" {
		t.Errorf("restarted %v, want prod/shop", f.restarted)
	}
	if m.busy != "" || !strings.Contains(m.message, "shop runs v4") {
		t.Errorf("busy %q, message %q", m.busy, m.message)
	}
}

func TestOneActionAtATime(t *testing.T) {
	m := start(t, &fake{})
	m.busy = "restarting shop"
	press(m, "r")
	press(m, "b")
	if m.question != nil || m.busy != "restarting shop" {
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

func TestEnterShowsLogsAndBackups(t *testing.T) {
	m := start(t, &fake{})
	cmd := press(m, "enter")
	if m.app == nil || m.app.key != (appKey{"prod", "shop"}) {
		t.Fatalf("app screen = %+v, want shop", m.app)
	}
	follow, load := cmd().(tea.BatchMsg)[0], cmd().(tea.BatchMsg)[1]
	run(m, load)
	if !m.app.backupsLoaded || len(m.app.backups) != 1 {
		t.Errorf("backups = %+v", m.app.backups)
	}
	// The first line, then the second, as Bubble Tea would deliver them.
	_, next := m.Update(follow())
	m.Update(next())
	if got := strings.Join(m.app.logs, "|"); got != "listening on :8080|GET / 200" {
		t.Errorf("logs = %q", got)
	}
	if view := m.View().Content; !strings.Contains(view, "GET / 200") || !strings.Contains(view, "postgres") {
		t.Errorf("the app screen does not show the logs and backups:\n%s", view)
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
		t.Error("a key does not close the help")
	}
	if cmd := press(m, "q"); cmd == nil || cmd() != tea.Quit() {
		t.Error("q does not quit")
	}
}

func TestWhyPrefersWhatMoinSaid(t *testing.T) {
	m := newModel(t.Context(), (&fake{}).cli())
	said := "some ssh noise\n✗ shop has no backups yet\n"
	if got := m.why(errors.New("exit status 1"), said); got != "shop has no backups yet" {
		t.Errorf("why = %q", got)
	}
	if got := m.why(errors.New("cannot reach prod over SSH"), ""); got != "cannot reach prod over SSH" {
		t.Errorf("why = %q", got)
	}
}

func TestTheListScrollsToTheSelection(t *testing.T) {
	m := newModel(t.Context(), CLI{Servers: []string{"prod"}})
	m.height = 12
	var apps []daemon.AppStatus
	for i := range 30 {
		apps = append(apps, daemon.AppStatus{Name: fmt.Sprintf("app-%02d", i)})
	}
	m.Update(statusMsg{server: "prod", status: daemon.Status{Apps: apps}})
	for range 25 {
		press(m, "down")
	}
	if view := m.View().Content; !strings.Contains(view, "app-25") {
		t.Errorf("the selected app is not on the screen:\n%s", view)
	}
	if lines := strings.Count(m.View().Content, "\n") + 1; lines != m.height {
		t.Errorf("the screen has %d lines, want %d", lines, m.height)
	}
}
