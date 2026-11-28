package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/protocol"
)

// tab is one of the three lists: apps, servers, backups.
type tab int

const (
	appsTab tab = iota
	serversTab
	backupsTab
)

var tabNames = []string{"Apps", "Servers", "Backups"}

// model is everything the screen shows. Update changes it, View draws it.
type model struct {
	ctx  context.Context
	cli  CLI
	open func(url string) error

	width, height int
	tab           tab

	servers []server
	// selected is the app of the apps and backups tabs, selectedServer
	// the server of the servers tab.
	selected       appKey
	selectedServer string
	// facts are the releases and backups of each app, asked when it is
	// selected.
	facts map[appKey]*facts

	// app is the app whose logs are open, nil on the lists.
	app *appScreen
	// screens counts the log screens opened, so what arrives for a closed
	// one is dropped.
	screens int

	picker   *picker
	question *question
	help     bool

	// action is what runs now, like a restart. Nothing else starts while
	// it runs.
	action *action
	// message is the result of the last action, a ✓ or ✗ line.
	message string
	// frame turns the spinners.
	frame int
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

// facts are what the side of an app shows beyond its status: its
// releases and its backups.
type facts struct {
	releases []daemon.Release
	backups  []protocol.DatabaseBackup
	err      string
	loaded   bool
	asked    time.Time
}

// action is a step on a server that runs now, like "restarting my-app":
// the app it runs on, the verb its row shows, and what the status line
// says.
type action struct {
	key   appKey
	verb  string
	what  string
	step  string
	since time.Time
	// said is every line of its log, to find why when it fails.
	said []string
}

func newModel(ctx context.Context, cli CLI) *model {
	m := &model{ctx: ctx, cli: cli, open: openBrowser, width: 80, height: 24, facts: map[appKey]*facts{}}
	for _, name := range cli.Servers {
		m.servers = append(m.servers, server{name: name})
	}
	if len(m.servers) > 0 {
		m.selectedServer = m.servers[0].name
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.askAll(), tick(), spin())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return m, m.pressed(msg.String())
	case tickMsg:
		return m, tea.Batch(m.askAll(), m.askFacts(), tick())
	case spinMsg:
		m.frame++
		return m, spin()
	case statusMsg:
		m.gotStatus(msg)
		return m, m.askFacts()
	case factsMsg:
		m.gotFacts(msg)
	case logLineMsg:
		return m, m.gotLogLine(msg)
	case logsEndMsg:
		m.gotLogsEnd(msg)
	case stepMsg:
		return m, m.gotStep(msg)
	case doneMsg:
		return m, m.gotDone(msg)
	case composeMsg:
		return m, m.gotCompose(msg)
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

// serverNamed is the server of the list called name.
func (m *model) serverNamed(name string) (server, bool) {
	for _, server := range m.servers {
		if server.name == name {
			return server, true
		}
	}
	return server{}, false
}

// current is the app the keys act on: the open one, else the selected
// one. On the servers tab there is none.
func (m *model) current() (appKey, bool) {
	if m.app != nil {
		return m.app.key, true
	}
	if m.tab == serversTab {
		return appKey{}, false
	}
	_, ok := m.appStatus(m.selected)
	return m.selected, ok
}

// move moves the selection of the tab by step, and stays at the ends.
func (m *model) move(step int) {
	if m.tab == serversTab {
		names := make([]string, len(m.servers))
		for i, server := range m.servers {
			names[i] = server.name
		}
		m.selectedServer = moved(names, m.selectedServer, step)
		return
	}
	m.selected = moved(m.apps(), m.selected, step)
}

// moved is the item step places from now in items, kept at the ends.
func moved[T comparable](items []T, now T, step int) T {
	if len(items) == 0 {
		return now
	}
	index := 0
	for i, item := range items {
		if item == now {
			index = i
		}
	}
	return items[min(max(index+step, 0), len(items)-1)]
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
