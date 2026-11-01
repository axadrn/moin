// Package tui is what `ops` opens without a command. It shows the same data
// as the web UI and the CLI, live from the control plane. This file holds the
// state, the keys and the commands; view.go draws it.
package tui

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
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
		program.Send(streamErrMsg{err})
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

type (
	serversMsg []client.Server
	appsMsg    []client.App
	eventsMsg  []client.Event
	errMsg     struct{ err error }
	// streamErrMsg means a live stream broke; it reconnects by itself.
	streamErrMsg struct{ err error }
	addedMsg     client.NewServer
	detailMsg    client.AppDetail
	// outputMsg replaces the output panel. follow means a deploy streams
	// into it.
	outputMsg struct {
		id     int64
		title  string
		note   string
		lines  []string
		follow bool
	}
	// lineMsg adds one line to the output panel it belongs to.
	lineMsg struct {
		id   int64
		line string
	}
	// followDoneMsg ends the deploy in an output panel.
	followDoneMsg struct {
		id     int64
		notice string
		err    error
	}
	noticeMsg string
	tickMsg   time.Time
	// settingsMsg is what the settings tab shows.
	settingsMsg settings
)

type tab int

const (
	tabServers tab = iota
	tabApps
	tabActivity
	tabTeam
	tabSettings
)

// tabs is how many tabs the header shows.
const tabs = 5

type mode int

const (
	modeList mode = iota
	modeAdd
	modeConfirmRemove
	modeConfirmRollback
	modeEnv
	modeImage
	modeDomains
	modeProtect
	modeSQL
	modeConfirmWrite
	modeBackups
	modeConfirmRestore
	modeConfirmSecurity
	// modeForm asks the fields of m.form one after the other.
	modeForm
	// modeConfirm waits for a yes to m.confirm.
	modeConfirm
	modeGitHub
)

// form asks for a few values in turn and hands them to submit.
type form struct {
	title  string
	fields []formField
	step   int
	values []string
	submit func(values []string) tea.Cmd
}

// formField is one question of a form. An empty answer takes value; a
// field that is not optional needs one of the two.
type formField struct {
	label, hint, value string
	secret, optional   bool
}

// confirmation is a question that runs its command after a yes, and goes
// back to the mode it came from either way.
type confirmation struct {
	question, detail, verb string
	danger                 bool
	back                   mode
	run                    tea.Cmd
}

// shared is what every copy of the model points to.
type shared struct {
	// program lets commands send messages after they returned, such as the
	// lines of a deploy they follow.
	program *tea.Program
	// outputs numbers the output panels, so the lines of a deploy never land
	// in the logs opened after it.
	outputs atomic.Int64
}

type model struct {
	api    *client.Client
	url    string
	dir    string
	ctx    context.Context
	shared *shared

	tab  tab
	mode mode
	// events is the activity of the workspace, newest first.
	events  []client.Event
	width   int
	height  int
	err     error
	notice  string
	loading bool
	// offline is the error of a broken live stream until it reconnects.
	offline error
	// frame turns the spinner while a deploy runs.
	frame int

	servers  []client.Server
	selected int
	input    string
	added    *client.NewServer
	// pendingSQL is a query that changes data, waiting for a yes.
	pendingSQL string
	// pendingRestore is a snapshot ID or a time, waiting for a yes.
	pendingRestore string
	// pendingSecurity is a change of a server's access, waiting for a yes.
	pendingSecurity securityChange
	// pendingRollback is the deploy a rollback goes to, 0 for the one
	// before the live one.
	pendingRollback int
	form            form
	confirm         confirmation

	apps        []client.App
	appSelected int
	// detail is the open app; nil shows the app list.
	detail *client.AppDetail
	// deployPick is the number of the deploy picked in the open app's
	// history, 0 for none.
	deployPick  int
	envShown    bool
	outputID    int64
	outputTitle string
	outputNote  string
	output      []string
	// running is set while a deploy streams into the output panel; runStep
	// is its newest step.
	running    bool
	runStarted time.Time
	runStep    string

	settings        settings
	settingSelected int

	team         client.Team
	teamErr      error
	teamSelected int
}

func (m model) Init() tea.Cmd {
	return tick(time.Second)
}

// tick redraws once a second so "last seen" stays current, and faster while
// a deploy runs so the spinner turns. Only one tick is ever waiting.
func tick(every time.Duration) tea.Cmd {
	return tea.Tick(every, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		if m.running {
			m.frame++
			return m, tick(120 * time.Millisecond)
		}
		return m, tick(time.Second)
	case serversMsg:
		m.servers, m.loading, m.offline = msg, false, nil
		m.selected = min(m.selected, max(len(m.servers)-1, 0))
		if m.tab == tabActivity {
			return m, m.loadActivity()
		}
	case appsMsg:
		m.apps, m.offline = msg, nil
		m.appSelected = min(m.appSelected, max(len(m.apps)-1, 0))
		if m.tab == tabActivity {
			return m, m.loadActivity()
		}
		if m.tab == tabSettings {
			return m, m.loadSettings()
		}
		if m.detail != nil {
			return m, m.loadDetail(m.detail.App.Name)
		}
	case eventsMsg:
		m.events = msg
	case teamMsg:
		m.team, m.teamErr = msg.team, msg.err
		m.teamSelected = min(m.teamSelected, max(len(m.teamRows())-1, 0))
	case settingsMsg:
		m.settings = settings(msg)
		m.settingSelected = min(m.settingSelected, max(len(m.settingRows())-1, 0))
	case detailMsg:
		detail := client.AppDetail(msg)
		m.detail = &detail
	case outputMsg:
		m.outputID, m.outputTitle, m.outputNote, m.output = msg.id, msg.title, msg.note, msg.lines
		m.running, m.runStep = msg.follow, "waiting for the server"
		if msg.follow {
			m.runStarted = time.Now()
		}
	case lineMsg:
		if msg.id == m.outputID {
			m.output = append(m.output, msg.line)
			if text, step := style.LogLine(msg.line); step {
				m.runStep = text
			}
		}
	case followDoneMsg:
		// The result shows even when the panel moved on to something else.
		m.notice, m.err = msg.notice, msg.err
		if msg.id != m.outputID {
			return m, nil
		}
		took := style.Muted.Render(" · " + duration(time.Since(m.runStarted)))
		m.running = false
		m.outputNote = style.Success.Render(style.SymbolDone+" live") + took
		if msg.err != nil {
			m.outputNote = style.Danger.Render(style.SymbolFailed+" failed") + took
		}
	case noticeMsg:
		m.notice = string(msg)
	case errMsg:
		m.err = msg.err
	case streamErrMsg:
		m.offline = msg.err
	case writeMsg:
		m.mode, m.pendingSQL = modeConfirmWrite, string(msg)
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
	// Notices and errors stay until the next key.
	m.notice, m.err = "", nil
	switch m.mode {
	case modeAdd:
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "enter":
			return m, m.addServer(strings.TrimSpace(m.input))
		case "backspace":
			m.input = dropLast(m.input)
		default:
			m.input += msg.Text
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
			return m, m.rollback(app.Name, m.pendingRollback)
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
			if input == "show" || input == "hide" {
				m.envShown = input == "show"
				return m, m.showEnv(app.Name, m.envShown)
			}
			return m, m.changeEnv(app.Name, input, m.envShown)
		case "backspace":
			m.input = dropLast(m.input)
		default:
			m.input += msg.Text
		}
		return m, nil
	case modeDomains, modeProtect:
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "enter":
			app, _ := m.currentApp()
			input := strings.TrimSpace(m.input)
			mode := m.mode
			m.mode, m.input = modeList, ""
			if mode == modeDomains {
				return m, m.changeDomains(app, input)
			}
			return m, m.changeProtect(app.Name, input)
		case "backspace":
			m.input = dropLast(m.input)
		default:
			m.input += msg.Text
		}
		return m, nil
	case modeSQL:
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "enter":
			app, _ := m.currentApp()
			query := strings.TrimSpace(m.input)
			if query == "" {
				return m, nil
			}
			return m, m.runSQL(app.Name, query, false)
		case "backspace":
			m.input = dropLast(m.input)
		default:
			m.input += msg.Text
		}
		return m, nil
	case modeBackups:
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "enter":
			app, _ := m.currentApp()
			target := strings.TrimSpace(m.input)
			m.input = ""
			if target == "" {
				return m, m.backUp(app.Name)
			}
			if target == "on" || target == "off" {
				return m, m.setSnapshots(app.Name, target == "on")
			}
			m.mode, m.pendingRestore = modeConfirmRestore, target
		case "backspace":
			m.input = dropLast(m.input)
		default:
			m.input += msg.Text
		}
		return m, nil
	case modeConfirmRestore:
		m.mode = modeBackups
		if app, ok := m.currentApp(); key == "y" && ok {
			return m, m.restore(app.Name, m.pendingRestore)
		}
		return m, nil
	case modeConfirmSecurity:
		m.mode = modeList
		if key == "y" {
			return m, m.applySecurity(m.pendingSecurity)
		}
		return m, nil
	case modeConfirm:
		m.mode = m.confirm.back
		if key == "y" {
			return m, m.confirm.run
		}
		return m, nil
	case modeForm:
		return m.formKey(msg)
	case modeGitHub:
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "enter":
			app, _ := m.currentApp()
			input := strings.TrimSpace(m.input)
			m.mode, m.input = modeList, ""
			return m, m.github(app, input)
		case "backspace":
			m.input = dropLast(m.input)
		default:
			m.input += msg.Text
		}
		return m, nil
	case modeConfirmWrite:
		m.mode = modeSQL
		if app, ok := m.currentApp(); key == "y" && ok {
			return m, m.runSQL(app.Name, m.pendingSQL, true)
		}
		return m, nil
	case modeImage:
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "enter":
			app, _ := m.currentApp()
			image := strings.TrimSpace(m.input)
			m.mode, m.input = modeList, ""
			if image == "" {
				return m, nil
			}
			return m, m.deployImage(app.Name, image)
		case "backspace":
			m.input = dropLast(m.input)
		default:
			m.input += msg.Text
		}
		return m, nil
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		// Esc closes what is open and never quits.
		switch {
		case m.added != nil:
			m.added = nil
		case m.detail != nil && m.deployPick != 0:
			m.deployPick = 0
		case m.detail != nil:
			m.detail, m.output, m.outputTitle, m.outputID, m.running = nil, nil, "", 0, false
		}
		return m, nil
	case "tab":
		m.tab = (m.tab + 1) % tabs
		m.added = nil
		switch m.tab {
		case tabActivity:
			return m, m.loadActivity()
		case tabTeam:
			return m, m.loadTeam()
		case tabSettings:
			return m, m.loadSettings()
		}
		return m, nil
	}
	switch m.tab {
	case tabServers:
		return m.serverKey(key)
	case tabActivity:
		return m, nil
	case tabTeam:
		return m.teamKey(key)
	case tabSettings:
		return m.settingKey(key)
	}
	return m.appKey(key)
}

// formKey types into the open form; enter takes the answer and asks the
// next field, after the last one it sends the form.
func (m model) formKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.input = modeList, ""
	case "enter":
		field := m.form.fields[m.form.step]
		value := strings.TrimSpace(m.input)
		if value == "" {
			value = field.value
		}
		if value == "" && !field.optional {
			m.err = fmt.Errorf("%s is needed", strings.ToLower(field.label))
			return m, nil
		}
		m.input = ""
		m.form.values = append(slices.Clone(m.form.values), value)
		m.form.step++
		if m.form.step < len(m.form.fields) {
			return m, nil
		}
		m.mode = modeList
		return m, m.form.submit(m.form.values)
	case "backspace":
		m.input = dropLast(m.input)
	default:
		m.input += msg.Text
	}
	return m, nil
}

// ask opens a form.
func (m model) ask(title string, submit func([]string) tea.Cmd, fields ...formField) model {
	m.mode, m.input = modeForm, ""
	m.form = form{title: title, fields: fields, submit: submit}
	return m
}

// confirmFirst asks a question before run.
func (m model) confirmFirst(question, detail, verb string, danger bool, run tea.Cmd) model {
	m.confirm = confirmation{question: question, detail: detail, verb: verb, danger: danger, back: m.mode, run: run}
	m.mode = modeConfirm
	return m
}

func dropLast(text string) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return text
	}
	return string(runes[:len(runes)-1])
}

func (m model) serverKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.selected = max(m.selected-1, 0)
	case "down", "j":
		m.selected = min(m.selected+1, max(len(m.servers)-1, 0))
	case "a":
		m.mode, m.added = modeAdd, nil
	case "x":
		if len(m.servers) > 0 {
			m.mode = modeConfirmRemove
		}
	case "u":
		if m.selected < len(m.servers) {
			server := m.servers[m.selected]
			m.notice = "updating the agent of " + server.Name + "…"
			return m, m.updateAgent(server)
		}
	case "t", "f", "s":
		if m.selected < len(m.servers) {
			server := m.servers[m.selected]
			change := securityChange{server: server, security: client.Security{Tunnel: server.Tunnel, SSHTailscale: server.SSHTailscale, Firewall: server.Firewall}}
			if key == "t" {
				change.security.Tunnel = !server.Tunnel
				change.question = "Route " + server.Name + " through a Cloudflare Tunnel?"
				change.detail = "Ports 80 and 443 close; ops points your domains at the tunnel."
				if server.Tunnel {
					change.question, change.detail = "Take "+server.Name+" off the tunnel?", "Traefik takes ports 80 and 443 again; point your domains at the server."
				}
			} else if key == "s" {
				change.security.SSHTailscale = !server.SSHTailscale
				change.question, change.detail = "Let "+server.Name+" answer SSH only on your tailnet?", "Port 22 closes once Tailscale is up; keep another way in, such as your provider's console."
				if server.SSHTailscale {
					change.question, change.detail = "Open SSH on "+server.Name+" to the internet again?", "Port 22 answers on every interface."
				}
			} else {
				change.security.Firewall = !server.Firewall
				change.question, change.detail = "Turn on the firewall of "+server.Name+"?", "Every inbound connection it does not need is dropped."
				if server.Firewall {
					change.question, change.detail = "Turn off the firewall of "+server.Name+"?", "Every port is reachable again."
				}
			}
			m.mode, m.pendingSecurity = modeConfirmSecurity, change
		}
	}
	return m, nil
}

// securityChange is a server's new access settings and the question
// that asks for them.
type securityChange struct {
	server           client.Server
	security         client.Security
	question, detail string
}

// applySecurity sends the settings and says what the agent set up.
func (m model) applySecurity(change securityChange) tea.Cmd {
	return func() tea.Msg {
		secured, err := m.api.SetSecurity(m.ctx, change.server.ID, change.security)
		if err != nil {
			return errMsg{err}
		}
		notice := "applied to " + secured.Name + ": " + secured.Access()
		if secured.Security != nil && len(secured.Security.Problems) > 0 {
			return errMsg{fmt.Errorf("%s; %s", notice, strings.Join(secured.Security.Problems, "; "))}
		}
		return noticeMsg(notice)
	}
}

// updateAgent puts the control plane's version of the agent on a server
// and waits until it is back.
func (m model) updateAgent(server client.Server) tea.Cmd {
	return func() tea.Msg {
		updated, err := m.api.UpdateAgent(m.ctx, server.ID)
		if err != nil {
			return errMsg{err}
		}
		return noticeMsg("the agent of " + updated.Name + " runs " + updated.AgentVersion)
	}
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
				m.output, m.outputTitle, m.outputID, m.running, m.deployPick = nil, "", 0, false, 0
				return m, m.loadDetail(app.Name)
			}
			return m, nil
		case "a":
			return m.addAppForm(), nil
		}
	} else {
		switch key {
		case "up", "k", "down", "j":
			m.deployPick = m.pickDeploy(key == "down" || key == "j")
			return m, nil
		case "enter":
			if m.deployPick != 0 {
				return m, m.deployLog(m.detail.App.Name, m.deployPick)
			}
			return m, nil
		}
	}
	app, ok := m.currentApp()
	switch key {
	case "d":
		if m.hasApp(client.AppName("", m.dir)) {
			return m, m.deploy()
		}
	case "l":
		if ok {
			return m, m.logs(app.Name)
		}
	case "r":
		if ok {
			if m.detail != nil && m.deployPick != 0 && m.deployPick == app.LiveNumber {
				m.err = fmt.Errorf("v%d is live already: pick an older deploy", app.LiveNumber)
				return m, nil
			}
			m.mode, m.pendingRollback = modeConfirmRollback, 0
			if m.detail != nil {
				m.pendingRollback = m.deployPick
			}
		}
	case "R":
		if ok {
			return m, m.redeploy(app.Name)
		}
	case "e":
		if ok {
			m.mode, m.input, m.envShown = modeEnv, "", false
			return m, m.showEnv(app.Name, false)
		}
	case "i":
		if ok {
			m.mode, m.input = modeImage, ""
		}
	case "h":
		if ok {
			return m, m.newHook(app.Name)
		}
	case "o":
		if ok {
			m.mode, m.input = modeDomains, ""
			return m, m.showDomains(app.Name)
		}
	case "p":
		if ok {
			m.mode, m.input = modeProtect, ""
		}
	case "s":
		if ok {
			m.mode, m.input = modeSQL, ""
			return m, m.showTables(app.Name)
		}
	case "b":
		if ok {
			m.mode, m.input = modeBackups, ""
			return m, m.showBackups(app.Name, "")
		}
	case "g":
		if ok {
			m.mode, m.input = modeGitHub, ""
			return m, m.showGitHub(app)
		}
	case "x":
		if ok {
			m.mode = modeConfirmRemove
		}
	}
	return m, nil
}

// hasApp reports whether the workspace has an app of that name.
func (m model) hasApp(name string) bool {
	return slices.ContainsFunc(m.apps, func(app client.App) bool { return app.Name == name })
}

// pickDeploy moves the pick in the open app's history one row down or up;
// up from the newest one picks none.
func (m model) pickDeploy(down bool) int {
	deploys := m.detail.Deploys[:min(len(m.detail.Deploys), historyRows)]
	index := slices.IndexFunc(deploys, func(deploy client.Deploy) bool { return deploy.Number == m.deployPick })
	switch {
	case len(deploys) == 0:
		return 0
	case down:
		return deploys[min(index+1, len(deploys)-1)].Number
	case index <= 0:
		return 0
	}
	return deploys[index-1].Number
}

// addAppForm asks for the name and the server of a new app. The name
// defaults to the directory's app, the server to the only one there is.
func (m model) addAppForm() model {
	here := client.AppName("", m.dir)
	if m.hasApp(here) {
		here = ""
	}
	server := client.ReadLocalConfig(m.dir).Server
	names := make([]string, 0, len(m.servers))
	for _, s := range m.servers {
		names = append(names, s.Name)
	}
	if server == "" && len(m.servers) == 1 {
		server = m.servers[0].Name
	}
	hint := "one of " + strings.Join(names, ", ")
	if len(names) == 0 {
		hint = "add a server first, on the servers tab"
	}
	return m.ask("New app", func(values []string) tea.Cmd { return m.addApp(values[0], values[1]) },
		formField{label: "Name", value: here, hint: placeholder(here, "lowercase letters, digits and dashes")},
		formField{label: "Server", value: server, hint: placeholder(server, hint)},
	)
}

// placeholder is what an empty answer takes, or a hint when there is none.
func placeholder(value, hint string) string {
	if value != "" {
		return "enter takes " + value
	}
	return hint
}

func (m model) addApp(name, server string) tea.Cmd {
	return func() tea.Msg {
		app, err := m.api.AddApp(m.ctx, name, server)
		if err != nil {
			return errMsg{err}
		}
		next := "Deploy it with i (an image), g (GitHub) or " + build.Name + " deploy in its repo."
		if app.Name == client.AppName("", m.dir) {
			next = "Press d to deploy " + m.dir + "."
		}
		return noticeMsg("Added " + app.Name + " on " + app.ServerName + ". " + next)
	}
}

// deployLog shows the log of one deploy; a running one streams on.
func (m model) deployLog(name string, number int) tea.Cmd {
	running := slices.ContainsFunc(m.detail.Deploys, func(deploy client.Deploy) bool {
		return deploy.Number == number && deploy.Status == "running"
	})
	return func() tea.Msg {
		if running {
			return m.follow(name, client.Deploy{Number: number}, fmt.Sprintf("deploy v%d of %s", number, name))
		}
		var lines []string
		done, err := m.api.FollowDeploy(m.ctx, name, number, func(line string) { lines = append(lines, line) })
		if err != nil {
			return errMsg{err}
		}
		note := style.Success.Render(style.SymbolDone + " " + done.Status)
		if done.Status != "succeeded" {
			note = style.Danger.Render(style.SymbolFailed + " " + done.Status)
		}
		if done.Error != "" {
			lines = append(lines, done.Error)
		}
		return outputMsg{id: m.shared.outputs.Add(1), title: fmt.Sprintf("log of v%d of %s", number, name), note: note, lines: lines}
	}
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
		return noticeMsg(name + " removed. Its data stays on the server")
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

// loadActivity fetches what the team did; every change of the servers or
// apps loads it again while the tab is open.
func (m model) loadActivity() tea.Cmd {
	return func() tea.Msg {
		events, err := m.api.Activity(m.ctx, 100)
		if err != nil {
			return errMsg{err}
		}
		return eventsMsg(events)
	}
}

func (m model) logs(name string) tea.Cmd {
	const tail = 200
	return func() tea.Msg {
		lines, err := m.api.Logs(m.ctx, name, tail)
		if err != nil {
			return errMsg{err}
		}
		if len(lines) == 0 {
			lines = []string{name + " has written no log lines yet"}
		}
		return outputMsg{id: m.shared.outputs.Add(1), title: "logs of " + name, note: style.Muted.Render(fmt.Sprintf("last %d lines per container", tail)), lines: lines}
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
		started, err := m.api.Deploy(m.ctx, name, source.Bytes())
		if client.IsNotFound(err) {
			return errMsg{fmt.Errorf("no app %s yet: run `%s deploy` in %s once", name, build.Name, m.dir)}
		}
		if err != nil {
			return errMsg{err}
		}
		return m.follow(name, started, fmt.Sprintf("deploy v%d of %s", started.Number, name))
	}
}

// deployImage deploys an image built elsewhere and follows it.
func (m model) deployImage(name, image string) tea.Cmd {
	return func() tea.Msg {
		started, err := m.api.DeployImage(m.ctx, name, image)
		if err != nil {
			return errMsg{err}
		}
		return m.follow(name, started, fmt.Sprintf("deploy v%d of %s", started.Number, name))
	}
}

// deployRef deploys a branch, tag or commit of the app's GitHub repo, or
// the linked branch for "".
func (m model) deployRef(name, ref string) tea.Cmd {
	return func() tea.Msg {
		started, err := m.api.DeployRef(m.ctx, name, ref)
		if err != nil {
			return errMsg{err}
		}
		title := fmt.Sprintf("deploy v%d of %s", started.Number, name)
		if started.Commit != "" {
			title += " from " + started.Commit[:min(7, len(started.Commit))]
		}
		return m.follow(name, started, title)
	}
}

// showGitHub fills the output panel with the repo the app follows and the
// repos ops may read.
func (m model) showGitHub(app client.App) tea.Cmd {
	return func() tea.Msg {
		repos, err := m.api.GitHubRepos(m.ctx)
		lines := []string{}
		switch {
		case app.Repo == "":
			lines = append(lines, app.Name+" follows no repo yet")
		case app.AutoDeploy:
			lines = append(lines, app.Name+" follows "+style.Bold.Render(app.Repo)+" on "+app.Branch+", and deploys every push")
		default:
			lines = append(lines, app.Name+" follows "+style.Bold.Render(app.Repo)+" on "+app.Branch+"; it deploys when you say so")
		}
		lines = append(lines, "")
		switch {
		case err != nil:
			lines = append(lines, style.Muted.Render("repos: "+sentence(err.Error())))
		case len(repos) == 0:
			lines = append(lines, style.Muted.Render("No repos yet: install the ops GitHub App, see the settings tab."))
		default:
			rows := make([][]any, 0, len(repos))
			for _, repo := range repos {
				visibility := "public"
				if repo.Private {
					visibility = "private"
				}
				rows = append(rows, []any{repo.FullName, repo.DefaultBranch, visibility})
			}
			lines = append(lines, resultLines([]string{"repo", "branch", "visibility"}, rows)...)
		}
		note := "type link owner/repo [branch] [manual] to follow one"
		if app.Repo != "" {
			note = "enter deploys " + app.Branch + " · a branch, tag or commit deploys that"
		}
		return outputMsg{id: m.shared.outputs.Add(1), title: "GitHub of " + app.Name, note: style.Muted.Render(note), lines: lines}
	}
}

// github answers the GitHub prompt: "" deploys the linked branch, "link
// owner/repo [branch] [manual]" follows a repo, "unlink" stops, anything
// else is a branch, tag or commit to deploy.
func (m model) github(app client.App, input string) tea.Cmd {
	fields := strings.Fields(input)
	switch {
	case len(fields) > 0 && fields[0] == "unlink":
		return func() tea.Msg {
			err := m.api.UnlinkRepo(m.ctx, app.Name)
			if err != nil {
				return errMsg{err}
			}
			return noticeMsg(app.Name + " follows no repo any more")
		}
	case len(fields) > 0 && fields[0] == "link":
		if len(fields) < 2 {
			return func() tea.Msg { return errMsg{fmt.Errorf("type link owner/repo, then optionally a branch and manual")} }
		}
		repo, branch, auto := fields[1], "", true
		for _, field := range fields[2:] {
			if field == "manual" {
				auto = false
			} else {
				branch = field
			}
		}
		return func() tea.Msg {
			linked, err := m.api.LinkRepo(m.ctx, app.Name, repo, branch, auto)
			if err != nil {
				return errMsg{err}
			}
			notice := app.Name + " follows " + linked.Repo + " on " + linked.Branch
			if linked.AutoDeploy {
				return noticeMsg(notice + " and deploys every push. Press g, then enter, to deploy it now")
			}
			return noticeMsg(notice + ". Press g, then enter, to deploy it")
		}
	}
	return m.deployRef(app.Name, input)
}

// showDomains fills the output panel with the domains and their DNS state.
func (m model) showDomains(name string) tea.Cmd {
	return func() tea.Msg {
		checks, err := m.api.CheckDomains(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		lines := []string{}
		advice := ""
		for _, check := range checks {
			advice = check.Advice
			state := "set DNS"
			if check.Points {
				state = "points here"
			}
			line := check.Domain + "   " + state
			if check.Canonical {
				line += "   canonical"
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			lines = append(lines, "no domains yet")
		}
		note := "type blog.de to add, -blog.de to remove, =blog.de for canonical"
		if advice != "" {
			note = "set DNS: " + advice
		}
		return outputMsg{id: m.shared.outputs.Add(1), title: "domains of " + name, note: style.Muted.Render(note), lines: lines}
	}
}

// changeDomains adds blog.de, removes -blog.de or sets =blog.de canonical.
func (m model) changeDomains(app client.App, input string) tea.Cmd {
	return func() tea.Msg {
		list, canonical := slices.Clone(app.Domains), app.Canonical
		switch {
		case strings.HasPrefix(input, "-"):
			domain := strings.TrimPrefix(input, "-")
			list = slices.DeleteFunc(list, func(d string) bool { return d == domain })
			if canonical == domain {
				canonical = ""
			}
		case strings.HasPrefix(input, "="):
			canonical = strings.TrimPrefix(input, "=")
			if canonical == "none" {
				canonical = ""
			}
		case input != "":
			list = append(list, input)
		}
		change, err := m.api.SetDomains(m.ctx, app.Name, list, canonical)
		if err != nil {
			return errMsg{err}
		}
		if change.Deploy != nil {
			return m.follow(app.Name, *change.Deploy, fmt.Sprintf("apply v%d of %s", change.Deploy.Number, app.Name))
		}
		return noticeMsg("Saved. The next deploy of " + app.Name + " applies it.")
	}
}

// changeProtect sets user:password, or turns protection off with "off".
func (m model) changeProtect(name, input string) tea.Cmd {
	return func() tea.Msg {
		var change client.Change
		var err error
		if input == "off" {
			change, err = m.api.RemoveProtection(m.ctx, name)
		} else {
			user, password, found := strings.Cut(input, ":")
			if !found {
				return errMsg{fmt.Errorf("type user:password, or off")}
			}
			change, err = m.api.SetProtection(m.ctx, name, user, password)
		}
		if err != nil {
			return errMsg{err}
		}
		if change.Deploy != nil {
			return m.follow(name, *change.Deploy, fmt.Sprintf("apply v%d of %s", change.Deploy.Number, name))
		}
		return noticeMsg("Saved. The next deploy of " + name + " applies it.")
	}
}

// newHook replaces the app's deploy hook and shows the new URL once.
func (m model) newHook(name string) tea.Cmd {
	return func() tea.Msg {
		url, err := m.api.RotateDeployHook(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		lines := []string{url, "", "Store it as a CI secret and call it after pushing the image:", `curl -fsS -X POST "$OPS_DEPLOY_HOOK?image=ghcr.io/me/` + name + `:$GITHUB_SHA"`}
		return outputMsg{id: m.shared.outputs.Add(1), title: "deploy hook of " + name, note: style.Warning.Render("shown once · the old one stops working"), lines: lines}
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

// showEnv fills the output panel with the env vars; values stay hidden
// unless shown is set.
func (m model) showEnv(name string, shown bool) tea.Cmd {
	return func() tea.Msg {
		env, err := m.api.Env(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return m.envOutput(name, env, shown)
	}
}

// changeEnv sets KEY=value or removes -KEY, then shows the env again.
func (m model) changeEnv(name, input string, shown bool) tea.Cmd {
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
		return m.envOutput(name, env, shown)
	}
}

func (m model) envOutput(name string, env map[string]string, shown bool) outputMsg {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		value := "••••••"
		if shown {
			value = env[key]
		}
		lines = append(lines, key+"="+value)
	}
	if len(lines) == 0 {
		lines = append(lines, "no env vars yet")
	}
	note := "values hidden · the next deploy gets changes"
	if shown {
		note = "values shown · the next deploy gets changes"
	}
	return outputMsg{id: m.shared.outputs.Add(1), title: "env of " + name, note: style.Muted.Render(note), lines: lines}
}

// rollback starts the images of deploy number again, or of the one before
// the live one for 0.
func (m model) rollback(name string, number int) tea.Cmd {
	return func() tea.Msg {
		started, err := m.api.Rollback(m.ctx, name, number)
		if err != nil {
			return errMsg{err}
		}
		return m.follow(name, started, fmt.Sprintf("rollback of %s to v%d", name, started.FromNumber))
	}
}

// follow opens the output panel and streams the deploy into it.
func (m model) follow(name string, started client.Deploy, title string) tea.Msg {
	program := m.shared.program
	id := m.shared.outputs.Add(1)
	go func() {
		done, err := m.api.FollowDeploy(m.ctx, name, started.Number, func(line string) {
			program.Send(lineMsg{id, line})
		})
		switch {
		case err != nil:
			program.Send(followDoneMsg{id: id, err: err})
		case done.Status != "succeeded":
			program.Send(followDoneMsg{id: id, err: fmt.Errorf("v%d of %s failed: %s", done.Number, name, done.Error)})
		default:
			program.Send(followDoneMsg{id: id, notice: fmt.Sprintf("v%d of %s is live", done.Number, name)})
		}
	}()
	return outputMsg{id: id, title: title, lines: []string{}, follow: true}
}

// writeMsg is a query that tried to change data; it waits for a yes.
type writeMsg string

// showTables lists the tables of the app's database in the output panel.
func (m model) showTables(name string) tea.Cmd {
	return func() tea.Msg {
		tables, err := m.api.Tables(m.ctx, name, "")
		if err != nil {
			return errMsg{err}
		}
		rows := [][]any{}
		for _, table := range tables.Tables {
			rows = append(rows, []any{table.Name, table.Type, float64(table.Rows)})
		}
		note := style.Muted.Render(tables.Database)
		if tables.Migration != nil {
			note += style.Muted.Render(" · " + tables.Migration.Tool + " at " + tables.Migration.Version)
		}
		return outputMsg{id: m.shared.outputs.Add(1), title: "data of " + name, note: note, lines: resultLines([]string{"table", "type", "rows"}, rows)}
	}
}

// runSQL runs a query. A read that would change data asks first.
func (m model) runSQL(name, query string, write bool) tea.Cmd {
	return func() tea.Msg {
		result, err := m.api.Query(m.ctx, name, "", query, write, 200)
		if err != nil && !write && strings.Contains(err.Error(), "readonly") {
			return writeMsg(query)
		}
		if err != nil {
			return errMsg{err}
		}
		if write {
			return noticeMsg(fmt.Sprintf("%d rows changed", result.Changes))
		}
		note := style.Muted.Render(fmt.Sprintf("%d rows", len(result.Rows)))
		if result.Truncated {
			note += style.Muted.Render(", more not shown")
		}
		return outputMsg{id: m.shared.outputs.Add(1), title: query, note: note, lines: resultLines(result.Columns, result.Rows)}
	}
}

// resultLines lays out rows as aligned columns under a muted header.
func resultLines(columns []string, rows [][]any) []string {
	cells := [][]string{make([]string, len(columns))}
	for index, column := range columns {
		cells[0][index] = strings.ToUpper(column)
	}
	for _, row := range rows {
		line := make([]string, len(row))
		for index, value := range row {
			line[index] = cellText(value)
		}
		cells = append(cells, line)
	}
	widths := make([]int, len(columns))
	for _, row := range cells {
		for index, cell := range row {
			if index < len(widths) {
				widths[index] = max(widths[index], lipgloss.Width(cell))
			}
		}
	}
	lines := make([]string, 0, len(cells))
	for number, row := range cells {
		parts := make([]string, len(row))
		for index, cell := range row {
			parts[index] = cell + strings.Repeat(" ", max(widths[min(index, len(widths)-1)]-lipgloss.Width(cell), 0))
		}
		line := strings.TrimRight(strings.Join(parts, "  "), " ")
		if number == 0 {
			line = style.Muted.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

func cellText(value any) string {
	switch value := value.(type) {
	case nil:
		return style.Muted.Render("NULL")
	case float64:
		if value == float64(int64(value)) {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'g', -1, 64)
	case string:
		value = strings.ReplaceAll(value, "\n", " ")
		if len([]rune(value)) > 40 {
			return string([]rune(value)[:39]) + "…"
		}
		return value
	}
	return fmt.Sprint(value)
}

// showBackups lists the backups of an app in the output panel.
func (m model) showBackups(name, notice string) tea.Cmd {
	return func() tea.Msg {
		list, err := m.api.Backups(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return backupsOutput(m.shared.outputs.Add(1), name, list)
	}
}

func backupsOutput(id int64, name string, list client.AppBackups) outputMsg {
	now := time.Now()
	lines := []string{}
	for _, database := range list.Databases {
		head := style.Bold.Render(database.Database)
		switch {
		case database.Error != "":
			head += "  " + style.Danger.Render(style.SymbolFailed+" "+database.Error)
		case database.VerifiedAt != nil:
			head += "  " + style.Success.Render(style.SymbolDone+" verified by restore "+duration(now.Sub(*database.VerifiedAt))+" ago")
		}
		lines = append(lines, head)
		if database.Since != nil && database.ReplicatedAt != nil {
			lines = append(lines, style.Muted.Render("point in time "+database.Since.UTC().Format("2006-01-02 15:04:05")+" to "+database.ReplicatedAt.UTC().Format("15:04:05")+" UTC"))
		}
		rows := [][]any{}
		for _, snapshot := range database.Snapshots {
			rows = append(rows, []any{snapshot.ID, snapshot.Reason, duration(now.Sub(snapshot.CreatedAt)) + " ago"})
		}
		lines = append(lines, resultLines([]string{"snapshot", "reason", "taken"}, rows)...)
		lines = append(lines, "")
	}
	note := style.Muted.Render("snapshot before every change: on")
	if !list.SnapshotBeforeChanges {
		note = style.Muted.Render("snapshot before every change: off")
	}
	return outputMsg{id: id, title: "backups of " + name, note: note, lines: lines}
}

// setSnapshots turns the snapshot before every change on or off and shows
// the backups again.
func (m model) setSnapshots(name string, on bool) tea.Cmd {
	return func() tea.Msg {
		err := m.api.SetSnapshotBeforeChanges(m.ctx, name, on)
		if err != nil {
			return errMsg{err}
		}
		notice := noticeMsg("snapshot before every change: off")
		if on {
			notice = "snapshot before every change: on"
		}
		list, err := m.api.Backups(m.ctx, name)
		if err != nil {
			// The setting is saved; the list needs the server online.
			return notice
		}
		m.shared.program.Send(notice)
		return backupsOutput(m.shared.outputs.Add(1), name, list)
	}
}

// backUp takes a snapshot and shows the backups again.
func (m model) backUp(name string) tea.Cmd {
	return func() tea.Msg {
		list, err := m.api.BackUp(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		m.shared.program.Send(noticeMsg("snapshot taken"))
		return backupsOutput(m.shared.outputs.Add(1), name, list)
	}
}

// restore restores a snapshot ID, or a time in UTC.
func (m model) restore(name, target string) tea.Cmd {
	return func() tea.Msg {
		snapshot := target
		var at *time.Time
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "15:04:05", "15:04"} {
			parsed, err := time.Parse(layout, target)
			if err != nil {
				continue
			}
			if parsed.Year() == 0 {
				now := time.Now().UTC()
				parsed = time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, time.UTC)
			}
			at, snapshot = &parsed, ""
			break
		}
		err := m.api.Restore(m.ctx, name, "", snapshot, at)
		if err != nil {
			return errMsg{err}
		}
		m.shared.program.Send(noticeMsg("restored; " + name + " runs again"))
		list, err := m.api.Backups(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return backupsOutput(m.shared.outputs.Add(1), name, list)
	}
}
