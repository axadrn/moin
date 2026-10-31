// Package tui is what `ops` opens without a command. It shows the same data
// as the web UI and the CLI, live from the control plane.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
)

// Run opens the TUI until the user quits or ctx ends.
func Run(ctx context.Context, api *client.Client, url string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	program := tea.NewProgram(model{api: api, url: url, ctx: ctx, loading: true}, tea.WithContext(ctx))
	go watch(ctx, api, program)
	_, err := program.Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// watch feeds the program with the live server list and reconnects when the
// stream breaks.
func watch(ctx context.Context, api *client.Client, program *tea.Program) {
	for ctx.Err() == nil {
		err := api.WatchServers(ctx, func(servers []client.Server) {
			program.Send(serversMsg(servers))
		})
		if ctx.Err() != nil {
			return
		}
		program.Send(errMsg{err})
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

type (
	serversMsg []client.Server
	errMsg     struct{ err error }
	addedMsg   client.NewServer
	tickMsg    time.Time
)

type mode int

const (
	modeList mode = iota
	modeAdd
	modeConfirmRemove
)

type model struct {
	api *client.Client
	url string
	ctx context.Context

	servers  []client.Server
	selected int
	loading  bool
	err      error
	mode     mode
	input    string
	added    *client.NewServer
	width    int
}

func (m model) Init() tea.Cmd {
	return tick()
}

// tick redraws once a second so "last seen" stays current.
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tickMsg:
		return m, tick()
	case serversMsg:
		m.servers, m.loading, m.err = msg, false, nil
		m.selected = min(m.selected, max(len(m.servers)-1, 0))
	case errMsg:
		m.err = msg.err
	case addedMsg:
		added := client.NewServer(msg)
		m.added, m.mode, m.input = &added, modeList, ""
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeAdd:
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "enter":
			return m, m.add(strings.TrimSpace(m.input))
		case "backspace":
			if m.input != "" {
				runes := []rune(m.input)
				m.input = string(runes[:len(runes)-1])
			}
		default:
			if msg.Text != "" {
				m.input += msg.Text
			}
		}
		return m, nil
	case modeConfirmRemove:
		m.mode = modeList
		if key == "y" && m.selected < len(m.servers) {
			return m, m.remove(m.servers[m.selected].ID)
		}
		return m, nil
	}
	switch key {
	case "q", "esc":
		if m.added != nil {
			m.added = nil
			return m, nil
		}
		return m, tea.Quit
	case "up", "k":
		m.selected = max(m.selected-1, 0)
	case "down", "j":
		m.selected = min(m.selected+1, max(len(m.servers)-1, 0))
	case "a":
		m.mode, m.added, m.err = modeAdd, nil, nil
	case "x":
		if len(m.servers) > 0 {
			m.mode = modeConfirmRemove
		}
	}
	return m, nil
}

func (m model) add(name string) tea.Cmd {
	return func() tea.Msg {
		created, err := m.api.AddServer(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return addedMsg(created)
	}
}

func (m model) remove(id string) tea.Cmd {
	return func() tea.Msg {
		err := m.api.RemoveServer(m.ctx, id)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

var (
	bar      = lipgloss.NewStyle().Reverse(true).Bold(true).Padding(0, 1)
	heading  = lipgloss.NewStyle().Bold(true)
	muted    = lipgloss.NewStyle().Faint(true)
	selected = lipgloss.NewStyle().Reverse(true)
	failure  = lipgloss.NewStyle().Bold(true)
)

func (m model) View() tea.View {
	var out strings.Builder
	out.WriteString(bar.Render(build.Name) + " " + muted.Render("servers · "+m.url) + "\n\n")

	switch {
	case m.loading && m.err == nil:
		out.WriteString(muted.Render("Connecting…") + "\n")
	case len(m.servers) == 0:
		out.WriteString("No servers yet. Press a to add one.\n")
	default:
		out.WriteString(m.table())
	}

	if m.added != nil {
		// The command stays on one unstyled line, so it copies in one piece.
		out.WriteString(fmt.Sprintf("\n%s added. Run this on the server as root within the next hour:\n\n%s\n", heading.Render(m.added.Server.Name), m.added.InstallCommand))
	}
	if m.err != nil {
		out.WriteString("\n" + failure.Render("! "+m.err.Error()) + "\n")
	}

	out.WriteString("\n")
	switch m.mode {
	case modeAdd:
		out.WriteString("Name: " + m.input + "█\n" + muted.Render("enter add · esc cancel"))
	case modeConfirmRemove:
		out.WriteString(fmt.Sprintf("Remove %s? Its agent is disconnected. ", heading.Render(m.servers[m.selected].Name)) + muted.Render("y yes · any other key no"))
	default:
		out.WriteString(muted.Render("↑↓ select · a add · x remove · q quit"))
	}

	view := tea.NewView(out.String())
	view.AltScreen = true
	return view
}

func (m model) table() string {
	now := time.Now()
	rows := [][]string{{"NAME", "STATUS", "SYSTEM", "AGENT", "LAST SEEN"}}
	for _, server := range m.servers {
		agentVersion := server.AgentVersion
		if agentVersion == "" {
			agentVersion = "-"
		}
		rows = append(rows, []string{server.Name, server.Status(), server.System(), agentVersion, server.LastSeen(now)})
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for column, cell := range row {
			widths[column] = max(widths[column], lipgloss.Width(cell))
		}
	}
	var out strings.Builder
	for index, row := range rows {
		cells := make([]string, len(row))
		for column, cell := range row {
			cells[column] = cell + strings.Repeat(" ", widths[column]-lipgloss.Width(cell))
		}
		line := strings.Join(cells, "   ")
		switch {
		case index == 0:
			line = muted.Render(line)
		case index-1 == m.selected:
			line = selected.Render(line)
		}
		out.WriteString(line + "\n")
	}
	return out.String()
}
