package tui

import (
	"bytes"
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// question is a y/n question before a step that changes a server.
type question struct {
	text string
	// yes is what runs on y.
	yes func() tea.Cmd
}

// picker lets you pick the release a rollback goes back to.
type picker struct {
	key      appKey
	releases []daemon.Release
	cursor   int
}

// releasesMsg is the releases of an app, for the picker.
type releasesMsg struct {
	key      appKey
	releases []daemon.Release
	err      error
	said     string
}

// doneMsg is how an action went: done says what it did, else err says
// why not.
type doneMsg struct {
	key  appKey
	done string
	err  error
	said string
}

// answer handles a key while a question is open: y runs it, any other
// key changes nothing.
func (m *model) answer(key string) tea.Cmd {
	asked := m.question
	m.question = nil
	if key == "y" || key == "Y" {
		return asked.yes()
	}
	m.message = style.Muted.Render("Nothing changed.")
	return nil
}

// ready says whether a new action may start: one runs at a time.
func (m *model) ready() (appKey, bool) {
	if m.busy != "" {
		return appKey{}, false
	}
	return m.current()
}

// askRestart asks before the app restarts.
func (m *model) askRestart() tea.Cmd {
	key, ok := m.ready()
	if !ok {
		return nil
	}
	m.question = &question{
		text: fmt.Sprintf("Restart %s on %s? It stops for a moment.", key.app, key.server),
		yes:  func() tea.Cmd { return m.restart(key) },
	}
	return nil
}

// restart runs the live release of the app again.
func (m *model) restart(key appKey) tea.Cmd {
	m.busy = "restarting " + key.app
	ctx, restart := m.ctx, m.cli.Restart
	return func() tea.Msg {
		var log bytes.Buffer
		result, err := restart(ctx, key.server, key.app, &log)
		return doneMsg{key: key, done: fmt.Sprintf("%s runs v%d", key.app, result.Release), err: err, said: log.String()}
	}
}

// askReleases fetches the releases of the app, for the picker.
func (m *model) askReleases() tea.Cmd {
	key, ok := m.ready()
	if !ok {
		return nil
	}
	m.busy = "asking " + key.server + " for the releases of " + key.app
	ctx, releases := m.ctx, m.cli.Releases
	return func() tea.Msg {
		var said bytes.Buffer
		list, err := releases(ctx, key.server, key.app, &said)
		return releasesMsg{key: key, releases: list, err: err, said: said.String()}
	}
}

// gotReleases opens the picker on the release before the live one, the
// one the CLI suggests too.
func (m *model) gotReleases(msg releasesMsg) {
	m.busy = ""
	if msg.err != nil {
		m.message = failed(m.why(msg.err, msg.said))
		return
	}
	for index, release := range msg.releases {
		if release.Before {
			m.picker = &picker{key: msg.key, releases: msg.releases, cursor: index}
			return
		}
	}
	m.message = failed(msg.key.app + " has no older release to go back to")
}

// pick handles a key in the release picker.
func (m *model) pick(key string) tea.Cmd {
	p := m.picker
	switch key {
	case "up", "k":
		p.cursor = max(p.cursor-1, 0)
	case "down", "j":
		p.cursor = min(p.cursor+1, len(p.releases)-1)
	case "enter":
		m.picker = nil
		number := p.releases[p.cursor].Number
		m.question = &question{
			text: fmt.Sprintf("Go back to v%d of %s? It runs with the env and domains as they are now.", number, p.key.app),
			yes:  func() tea.Cmd { return m.rollback(p.key, number) },
		}
	case "esc", "q":
		m.picker = nil
	}
	return nil
}

// rollback runs release number of the app again.
func (m *model) rollback(key appKey, number int) tea.Cmd {
	m.busy = "rolling back " + key.app
	ctx, rollback := m.ctx, m.cli.Rollback
	return func() tea.Msg {
		var log bytes.Buffer
		result, err := rollback(ctx, key.server, key.app, number, &log)
		return doneMsg{key: key, done: fmt.Sprintf("%s runs v%d again, as v%d", key.app, result.From, result.Release), err: err, said: log.String()}
	}
}

// gotDone shows how an action went and asks its server again, so the
// list shows the new release.
func (m *model) gotDone(msg doneMsg) tea.Cmd {
	m.busy = ""
	if msg.err != nil {
		m.message = failed(m.why(msg.err, msg.said))
		return nil
	}
	m.message = succeeded(msg.done)
	if msg.key.server == "" {
		return nil
	}
	return m.ask(msg.key.server)
}

// openAddress opens the app's first address in the browser.
func (m *model) openAddress() tea.Cmd {
	key, ok := m.current()
	if !ok {
		return nil
	}
	app, _ := m.appStatus(key)
	if len(app.Addresses) == 0 {
		m.message = failed(fmt.Sprintf("%s has no address yet. Add one with `%s domains add %s <domain>`", key.app, build.Name, key.app))
		return nil
	}
	address, open := app.Addresses[0], m.open
	return func() tea.Msg {
		if err := open(address); err != nil {
			return doneMsg{done: "Open it in your browser: " + address}
		}
		return doneMsg{done: "Opened " + address}
	}
}

func succeeded(text string) string {
	return style.Success.Render(style.SymbolDone) + " " + text
}

func failed(text string) string {
	return style.Danger.Render(style.SymbolFailed) + " " + text
}

// releaseName is a release as people write it, like v3.
func releaseName(number int) string {
	return "v" + strconv.Itoa(number)
}
