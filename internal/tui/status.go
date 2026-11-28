package tui

import (
	"bytes"
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/protocol"
)

// refreshEvery is how often the screen asks every server again, and
// factsEvery how often it asks again for the facts of an app it shows.
const (
	refreshEvery = 5 * time.Second
	factsEvery   = 30 * time.Second
)

// askTimeout is how long one server may take to answer.
const askTimeout = 15 * time.Second

// spinEvery is how fast the spinners turn.
const spinEvery = 120 * time.Millisecond

// tickMsg says it is time to ask the servers again.
type tickMsg struct{}

// spinMsg turns the spinners one step.
type spinMsg struct{}

// statusMsg is what one server answered, or why it did not.
type statusMsg struct {
	server string
	status daemon.Status
	err    error
}

// factsMsg is what a server said about the releases and backups of an
// app.
type factsMsg struct {
	key      appKey
	releases []daemon.Release
	backups  []protocol.DatabaseBackup
	err      error
	said     string
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func spin() tea.Cmd {
	return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} })
}

// askAll asks every server that is not still answering the last time. Each
// answers on its own, so a slow one holds up no other.
func (m *model) askAll() tea.Cmd {
	var asks []tea.Cmd
	for i := range m.servers {
		if !m.servers[i].asking {
			asks = append(asks, m.ask(m.servers[i].name))
		}
	}
	return tea.Batch(asks...)
}

// ask asks one server what runs there.
func (m *model) ask(name string) tea.Cmd {
	for i := range m.servers {
		if m.servers[i].name == name {
			m.servers[i].asking = true
		}
	}
	ctx, status := m.ctx, m.cli.Status
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, askTimeout)
		defer cancel()
		answer, err := status(ctx, name)
		return statusMsg{server: name, status: answer, err: err}
	}
}

// gotStatus keeps what a server answered.
func (m *model) gotStatus(msg statusMsg) {
	for i := range m.servers {
		if m.servers[i].name == msg.server {
			m.servers[i] = server{name: msg.server, status: msg.status, err: msg.err, answered: true}
		}
	}
	m.keepSelection()
}

// askFacts asks for the facts the screen shows and does not have fresh:
// of the selected app, and on the backups tab of every app.
func (m *model) askFacts() tea.Cmd {
	keys := []appKey{m.selected}
	if m.tab == backupsTab {
		keys = m.apps()
	}
	var asks []tea.Cmd
	for _, key := range keys {
		if _, ok := m.appStatus(key); !ok {
			continue
		}
		known := m.facts[key]
		if known != nil && time.Since(known.asked) < factsEvery {
			continue
		}
		if known == nil {
			known = &facts{}
			m.facts[key] = known
		}
		known.asked = time.Now()
		asks = append(asks, m.askFactsOf(key))
	}
	return tea.Batch(asks...)
}

// askFactsOf asks the server of key for the releases and backups of its
// app.
func (m *model) askFactsOf(key appKey) tea.Cmd {
	ctx, releases, backups := m.ctx, m.cli.Releases, m.cli.Backups
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, askTimeout)
		defer cancel()
		var said bytes.Buffer
		list, err := releases(ctx, key.server, key.app, &said)
		if err != nil {
			return factsMsg{key: key, err: err, said: said.String()}
		}
		databases, err := backups(ctx, key.server, key.app, &said)
		return factsMsg{key: key, releases: list, backups: databases, err: err, said: said.String()}
	}
}

// gotFacts keeps the facts of an app.
func (m *model) gotFacts(msg factsMsg) {
	known := m.facts[msg.key]
	if known == nil {
		known = &facts{asked: time.Now()}
		m.facts[msg.key] = known
	}
	known.loaded, known.err = true, ""
	if msg.err != nil {
		known.err = m.why(msg.err, msg.said)
		return
	}
	known.releases, known.backups = msg.releases, msg.backups
}

// forget drops the facts of an app, so the screen asks for them again,
// as after an action changed them.
func (m *model) forget(key appKey) {
	delete(m.facts, key)
}
