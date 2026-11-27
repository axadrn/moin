package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/daemon"
)

// refreshEvery is how often the screen asks every server again.
const refreshEvery = 5 * time.Second

// askTimeout is how long one server may take to answer.
const askTimeout = 15 * time.Second

// tickMsg says it is time to ask the servers again.
type tickMsg struct{}

// statusMsg is what one server answered, or why it did not.
type statusMsg struct {
	server string
	status daemon.Status
	err    error
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
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
