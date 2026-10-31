// Package tui is what `ops` opens without a command. It shows the same data
// as the web UI and the CLI, live from the control plane.
package tui

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
)

// Run opens the TUI until the user quits or ctx ends. dir is where `d`
// deploys from.
func Run(ctx context.Context, api *client.Client, url, dir string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	shared := &shared{}
	program := tea.NewProgram(model{api: api, url: url, dir: dir, ctx: ctx, shared: shared, loading: true}, tea.WithContext(ctx))
	shared.program = program
	go watch(ctx, program, func(ctx context.Context) error {
		return api.WatchServers(ctx, func(servers []client.Server) { program.Send(serversMsg(servers)) })
	})
	go watch(ctx, program, func(ctx context.Context) error {
		return api.WatchApps(ctx, func(apps []client.App) { program.Send(appsMsg(apps)) })
	})
	_, err := program.Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// watch runs a stream and reconnects when it breaks.
func watch(ctx context.Context, program *tea.Program, stream func(context.Context) error) {
	for ctx.Err() == nil {
		err := stream(ctx)
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
	appsMsg    []client.App
	errMsg     struct{ err error }
	addedMsg   client.NewServer
	detailMsg  client.AppDetail
	// outputMsg replaces the output panel.
	outputMsg struct {
		title string
		lines []string
	}
	// lineMsg adds one line to the output panel.
	lineMsg   string
	noticeMsg string
	tickMsg   time.Time
)

type tab int

const (
	tabServers tab = iota
	tabApps
)

type mode int

const (
	modeList mode = iota
	modeAdd
	modeConfirmRemove
	modeConfirmRollback
	modeEnv
)

// shared is what every copy of the model points to.
type shared struct {
	// program lets commands send messages after they returned, such as the
	// lines of a deploy they follow.
	program *tea.Program
}

type model struct {
	api    *client.Client
	url    string
	dir    string
	ctx    context.Context
	shared *shared

	tab     tab
	mode    mode
	width   int
	height  int
	err     error
	notice  string
	loading bool

	servers  []client.Server
	selected int
	input    string
	added    *client.NewServer

	apps        []client.App
	appSelected int
	// detail is the open app; nil shows the app list.
	detail      *client.AppDetail
	outputTitle string
	output      []string
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
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m, tick()
	case serversMsg:
		m.servers, m.loading, m.err = msg, false, nil
		m.selected = min(m.selected, max(len(m.servers)-1, 0))
	case appsMsg:
		m.apps = msg
		m.appSelected = min(m.appSelected, max(len(m.apps)-1, 0))
		if m.detail != nil {
			return m, m.loadDetail(m.detail.App.Name)
		}
	case detailMsg:
		detail := client.AppDetail(msg)
		m.detail = &detail
	case outputMsg:
		m.outputTitle, m.output = msg.title, msg.lines
	case lineMsg:
		m.output = append(m.output, string(msg))
	case noticeMsg:
		m.notice = string(msg)
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
			return m, m.addServer(strings.TrimSpace(m.input))
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
		if key != "y" {
			return m, nil
		}
		if m.tab == tabServers && m.selected < len(m.servers) {
			return m, m.removeServer(m.servers[m.selected].ID)
		}
		if app, ok := m.currentApp(); m.tab == tabApps && ok {
			m.detail = nil
			return m, m.removeApp(app.Name)
		}
		return m, nil
	case modeConfirmRollback:
		m.mode = modeList
		if app, ok := m.currentApp(); key == "y" && ok {
			m.err, m.notice = nil, ""
			return m, m.rollback(app.Name)
		}
		return m, nil
	case modeEnv:
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "enter":
			app, _ := m.currentApp()
			input := strings.TrimSpace(m.input)
			m.input = ""
			return m, m.changeEnv(app.Name, input)
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
	}

	switch key {
	case "q", "esc":
		switch {
		case m.added != nil:
			m.added = nil
		case m.detail != nil:
			m.detail, m.output, m.outputTitle = nil, nil, ""
		default:
			return m, tea.Quit
		}
		return m, nil
	case "tab":
		m.tab = (m.tab + 1) % 2
		m.added, m.err, m.notice = nil, nil, ""
		return m, nil
	}
	if m.tab == tabServers {
		return m.serverKey(key)
	}
	return m.appKey(key)
}

func (m model) serverKey(key string) (tea.Model, tea.Cmd) {
	switch key {
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

func (m model) appKey(key string) (tea.Model, tea.Cmd) {
	if m.detail == nil {
		switch key {
		case "up", "k":
			m.appSelected = max(m.appSelected-1, 0)
			return m, nil
		case "down", "j":
			m.appSelected = min(m.appSelected+1, max(len(m.apps)-1, 0))
			return m, nil
		case "enter":
			if app, ok := m.currentApp(); ok {
				m.output, m.outputTitle = nil, ""
				return m, m.loadDetail(app.Name)
			}
			return m, nil
		}
	}
	app, ok := m.currentApp()
	switch key {
	case "d":
		m.err, m.notice = nil, ""
		return m, m.deploy()
	case "l":
		if ok {
			m.err, m.notice = nil, ""
			return m, m.logs(app.Name)
		}
	case "r":
		if ok {
			m.mode = modeConfirmRollback
		}
	case "R":
		if ok {
			m.err, m.notice = nil, ""
			return m, m.redeploy(app.Name)
		}
	case "e":
		if ok {
			m.mode, m.input, m.err, m.notice = modeEnv, "", nil, ""
			return m, m.showEnv(app.Name)
		}
	case "x":
		if ok {
			m.mode = modeConfirmRemove
		}
	}
	return m, nil
}

// currentApp is the open app, or the selected one in the list.
func (m model) currentApp() (client.App, bool) {
	if m.detail != nil {
		return m.detail.App, true
	}
	if m.appSelected < len(m.apps) {
		return m.apps[m.appSelected], true
	}
	return client.App{}, false
}

func (m model) addServer(name string) tea.Cmd {
	return func() tea.Msg {
		created, err := m.api.AddServer(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return addedMsg(created)
	}
}

func (m model) removeServer(id string) tea.Cmd {
	return func() tea.Msg {
		err := m.api.RemoveServer(m.ctx, id)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m model) removeApp(name string) tea.Cmd {
	return func() tea.Msg {
		err := m.api.RemoveApp(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return noticeMsg(name + " removed. Its data stays on the server.")
	}
}

func (m model) loadDetail(name string) tea.Cmd {
	return func() tea.Msg {
		detail, err := m.api.App(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return detailMsg(detail)
	}
}

func (m model) logs(name string) tea.Cmd {
	return func() tea.Msg {
		lines, err := m.api.Logs(m.ctx, name, 200)
		if err != nil {
			return errMsg{err}
		}
		return outputMsg{title: "logs of " + name, lines: lines}
	}
}

// deploy packs the directory the TUI started in and follows the deploy in
// the output panel. The app must exist; `ops deploy` creates new ones.
func (m model) deploy() tea.Cmd {
	name := client.AppName("", m.dir)
	return func() tea.Msg {
		var source bytes.Buffer
		_, err := client.Pack(m.dir, &source)
		if err != nil {
			return errMsg{err}
		}
		started, err := m.api.Deploy(m.ctx, name, &source)
		if client.IsNotFound(err) {
			return errMsg{fmt.Errorf("no app %s yet: run `%s deploy` in %s once", name, build.Name, m.dir)}
		}
		if err != nil {
			return errMsg{err}
		}
		return m.follow(name, started, fmt.Sprintf("deploy v%d of %s", started.Number, name))
	}
}

func (m model) redeploy(name string) tea.Cmd {
	return func() tea.Msg {
		started, err := m.api.Redeploy(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return m.follow(name, started, fmt.Sprintf("redeploy v%d of %s", started.Number, name))
	}
}

// showEnv fills the output panel with the env keys; values stay hidden.
func (m model) showEnv(name string) tea.Cmd {
	return func() tea.Msg {
		env, err := m.api.Env(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return envOutput(name, env)
	}
}

// changeEnv sets KEY=value or removes -KEY, then shows the env again.
func (m model) changeEnv(name, input string) tea.Cmd {
	return func() tea.Msg {
		var set map[string]string
		var unset []string
		if key, ok := strings.CutPrefix(input, "-"); ok {
			unset = []string{key}
		} else if key, value, ok := strings.Cut(input, "="); ok {
			set = map[string]string{key: value}
		} else {
			return errMsg{fmt.Errorf("type KEY=value to set or -KEY to remove")}
		}
		env, err := m.api.SetEnv(m.ctx, name, set, unset)
		if err != nil {
			return errMsg{err}
		}
		return envOutput(name, env)
	}
}

func envOutput(name string, env map[string]string) outputMsg {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"=••••••")
	}
	if len(lines) == 0 {
		lines = append(lines, "no env vars yet")
	}
	return outputMsg{title: "env of " + name + " · the next deploy gets changes", lines: lines}
}

func (m model) rollback(name string) tea.Cmd {
	return func() tea.Msg {
		started, err := m.api.Rollback(m.ctx, name, 0)
		if err != nil {
			return errMsg{err}
		}
		return m.follow(name, started, fmt.Sprintf("rollback of %s to v%d", name, started.FromNumber))
	}
}

// follow opens the output panel and streams the deploy into it.
func (m model) follow(name string, started client.Deploy, title string) tea.Msg {
	program := m.shared.program
	go func() {
		done, err := m.api.FollowDeploy(m.ctx, name, started.Number, func(line string) {
			program.Send(lineMsg(line))
		})
		switch {
		case err != nil:
			program.Send(errMsg{err})
		case done.Status != "succeeded":
			program.Send(errMsg{fmt.Errorf("v%d failed: %s", done.Number, done.Error)})
		default:
			program.Send(noticeMsg(fmt.Sprintf("v%d of %s is live", done.Number, name)))
		}
	}()
	return outputMsg{title: title, lines: []string{}}
}

var (
	bar      = lipgloss.NewStyle().Reverse(true).Bold(true).Padding(0, 1)
	heading  = lipgloss.NewStyle().Bold(true)
	muted    = lipgloss.NewStyle().Faint(true)
	selected = lipgloss.NewStyle().Reverse(true)
	failure  = lipgloss.NewStyle().Bold(true)
)

func (m model) View() tea.View {
	var body strings.Builder
	tabs := []string{"servers", "apps"}
	tabs[m.tab] = heading.Render("[" + tabs[m.tab] + "]")
	body.WriteString(bar.Render(build.Name) + " " + strings.Join(tabs, " ") + " " + muted.Render(m.url) + "\n\n")

	switch {
	case m.tab == tabServers:
		body.WriteString(m.serversView())
	case m.detail != nil:
		body.WriteString(m.detailView())
	default:
		body.WriteString(m.appsView())
	}

	// The footer always shows: notices, errors and the keys or the question
	// waiting for an answer. The body gets the rows that are left.
	var footer strings.Builder
	if m.notice != "" {
		footer.WriteString("\n" + m.notice + "\n")
	}
	if m.err != nil {
		footer.WriteString("\n" + failure.Render("! "+m.err.Error()) + "\n")
	}
	footer.WriteString("\n" + m.help())
	footerLines := strings.Count(footer.String(), "\n") + 1

	lines := strings.Split(strings.TrimRight(body.String(), "\n"), "\n")
	if m.outputTitle != "" && m.tab == tabApps {
		output := m.output
		room := m.height - len(lines) - footerLines - 2
		if m.height == 0 {
			room = len(output)
		}
		room = max(room, 3)
		if len(output) > room {
			output = output[len(output)-room:]
		}
		lines = append(lines, "", heading.Render(m.outputTitle))
		for _, line := range output {
			lines = append(lines, muted.Render("  "+clip(line, m.width-2)))
		}
	}
	if available := m.height - footerLines; m.height > 0 && len(lines) > available {
		lines = lines[:max(available, 1)]
	}

	view := tea.NewView(strings.Join(lines, "\n") + "\n" + footer.String())
	view.AltScreen = true
	return view
}

func (m model) help() string {
	switch m.mode {
	case modeAdd:
		return "Name: " + m.input + "█\n" + muted.Render("enter add · esc cancel")
	case modeConfirmRemove:
		name := ""
		if m.tab == tabServers && m.selected < len(m.servers) {
			name = m.servers[m.selected].Name + "? Its agent is disconnected."
		} else if app, ok := m.currentApp(); ok {
			name = app.Name + "? Its containers stop, its data stays."
		}
		return "Remove " + name + " " + muted.Render("y yes · any other key no")
	case modeEnv:
		return "Env: " + m.input + "█\n" + muted.Render("KEY=value set · -KEY remove · enter apply · esc close")
	case modeConfirmRollback:
		app, _ := m.currentApp()
		return fmt.Sprintf("Roll %s back to the deploy before v%d? ", app.Name, app.LiveNumber) + muted.Render("y yes · any other key no")
	}
	switch {
	case m.tab == tabServers:
		return muted.Render("tab apps · ↑↓ select · a add · x remove · q quit")
	case m.detail != nil:
		return muted.Render("d deploy " + client.AppName("", m.dir) + " · R redeploy · l logs · e env · r rollback · x remove · esc back · q quit")
	}
	return muted.Render("tab servers · ↑↓ select · enter open · d deploy " + client.AppName("", m.dir) + " · R redeploy · l logs · e env · r rollback · x remove · q quit")
}

func (m model) serversView() string {
	var out strings.Builder
	switch {
	case m.loading && m.err == nil:
		out.WriteString(muted.Render("Connecting…") + "\n")
	case len(m.servers) == 0:
		out.WriteString("No servers yet. Press a to add one.\n")
	default:
		now := time.Now()
		rows := [][]string{{"NAME", "STATUS", "SYSTEM", "AGENT", "LAST SEEN"}}
		for _, server := range m.servers {
			rows = append(rows, []string{server.Name, server.Status(), server.System(), dash(server.AgentVersion), server.LastSeen(now)})
		}
		out.WriteString(table(rows, m.selected))
	}
	if m.added != nil {
		// The command stays on one unstyled line, so it copies in one piece.
		out.WriteString(fmt.Sprintf("\n%s added. Run this on the server as root within the next hour:\n\n%s\n", heading.Render(m.added.Server.Name), m.added.InstallCommand))
	}
	return out.String()
}

func (m model) appsView() string {
	if len(m.apps) == 0 {
		return fmt.Sprintf("No apps yet. Press d to deploy %s, or run `%s deploy` in a repo.\n", m.dir, build.Name)
	}
	rows := [][]string{{"NAME", "SERVER", "STATUS"}}
	for _, app := range m.apps {
		rows = append(rows, []string{app.Name, app.ServerName, app.Status()})
	}
	return table(rows, m.appSelected)
}

func (m model) detailView() string {
	app := m.detail.App
	var out strings.Builder
	out.WriteString(heading.Render(app.Name) + " on " + app.ServerName + " · " + app.Status() + "\n")
	for _, deploy := range m.detail.Deploys {
		if deploy.Status != "succeeded" || deploy.Detected == nil {
			continue
		}
		detected := deploy.Detected
		out.WriteString(muted.Render(fmt.Sprintf("%s · service %s · port %d", detected.Kind, detected.Service, detected.Port)) + "\n")
		for _, database := range detected.Databases {
			out.WriteString(muted.Render("database "+database.Path+" · "+database.HostPath) + "\n")
		}
		break
	}
	out.WriteString("\n")
	rows := [][]string{{"DEPLOY", "STATUS", "STARTED", "NOTE"}}
	for _, deploy := range m.detail.Deploys[:min(len(m.detail.Deploys), 8)] {
		note := deploy.Error
		if deploy.FromNumber > 0 {
			note = fmt.Sprintf("rollback to v%d %s", deploy.FromNumber, note)
		}
		rows = append(rows, []string{fmt.Sprintf("v%d", deploy.Number), deploy.Status, deploy.CreatedAt.Local().Format("Jan 2 15:04"), clip(note, 60)})
	}
	if len(rows) == 1 {
		return out.String() + "No deploys yet.\n"
	}
	return out.String() + table(rows, -1)
}

// table aligns rows; the first is the header. selectedRow is an index into
// the rows after the header, or -1.
func table(rows [][]string, selectedRow int) string {
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
		case index-1 == selectedRow:
			line = selected.Render(line)
		}
		out.WriteString(line + "\n")
	}
	return out.String()
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func clip(text string, width int) string {
	if width <= 1 || lipgloss.Width(text) <= width {
		return text
	}
	runes := []rune(text)
	return string(runes[:max(width-1, 0)]) + "…"
}
