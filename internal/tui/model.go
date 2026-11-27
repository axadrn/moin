package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/daemon"
)

// model is everything the screen shows. Update changes it, View draws it.
type model struct {
	ctx  context.Context
	cli  CLI
	open func(url string) error

	width, height int

	servers  []server
	selected appKey
	// app is the app opened with enter, nil on the list.
	app *appScreen
	// screens counts the app screens opened, so what arrives for a closed
	// one is dropped.
	screens int

	picker   *picker
	question *question
	help     bool
	// busy is what runs now, like "restarting shop". Nothing else starts
	// while it runs.
	busy string
	// message is the result of the last action, a ✓ or ✗ line.
	message string
}

// server is one server of the list and what it said last.
type server struct {
	name   string
	status daemon.Status
	err    error
	// answered is false until the first answer or error.
	answered bool
	// asking is true while a question to it is on its way.
	asking bool
}

// appKey names one app on one server.
type appKey struct {
	server, app string
}

func newModel(ctx context.Context, cli CLI) *model {
	m := &model{ctx: ctx, cli: cli, open: openBrowser, width: 80, height: 24}
	for _, name := range cli.Servers {
		m.servers = append(m.servers, server{name: name})
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.askAll(), tick())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return m, m.pressed(msg.String())
	case tickMsg:
		return m, tea.Batch(m.askAll(), tick())
	case statusMsg:
		m.gotStatus(msg)
	case logLineMsg:
		return m, m.gotLogLine(msg)
	case logsEndMsg:
		m.gotLogsEnd(msg)
	case backupsMsg:
		m.gotBackups(msg)
	case releasesMsg:
		m.gotReleases(msg)
	case doneMsg:
		return m, m.gotDone(msg)
	}
	return m, nil
}

// apps are every app the screen lists, server by server.
func (m *model) apps() []appKey {
	var keys []appKey
	for _, server := range m.servers {
		if server.err != nil {
			continue
		}
		for _, app := range server.status.Apps {
			keys = append(keys, appKey{server.name, app.Name})
		}
	}
	return keys
}

// appStatus is what the server of key said last about its app.
func (m *model) appStatus(key appKey) (daemon.AppStatus, bool) {
	for _, server := range m.servers {
		if server.name != key.server || server.err != nil {
			continue
		}
		for _, app := range server.status.Apps {
			if app.Name == key.app {
				return app, true
			}
		}
	}
	return daemon.AppStatus{}, false
}

// current is the app the keys act on: the open one, else the selected one.
func (m *model) current() (appKey, bool) {
	if m.app != nil {
		return m.app.key, true
	}
	_, ok := m.appStatus(m.selected)
	return m.selected, ok
}

// move moves the selection by step, and stays at the ends.
func (m *model) move(step int) {
	keys := m.apps()
	if len(keys) == 0 {
		return
	}
	index := 0
	for i, key := range keys {
		if key == m.selected {
			index = i
		}
	}
	index = min(max(index+step, 0), len(keys)-1)
	m.selected = keys[index]
}

// keepSelection selects the first app when the selected one is gone.
func (m *model) keepSelection() {
	if _, ok := m.appStatus(m.selected); ok {
		return
	}
	m.selected = appKey{}
	if keys := m.apps(); len(keys) > 0 {
		m.selected = keys[0]
	}
}
