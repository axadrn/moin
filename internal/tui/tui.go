// Package tui is what `ops` opens without a command. It shows the same data
// as the web UI and the CLI, live from the control plane. This file holds the
// state, the keys and the commands; view.go draws it.
package tui

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
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
	modeImage
)

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

	tab     tab
	mode    mode
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

	apps        []client.App
	appSelected int
	// detail is the open app; nil shows the app list.
	detail      *client.AppDetail
	outputID    int64
	outputTitle string
	outputNote  string
	output      []string
	// running is set while a deploy streams into the output panel; runStep
	// is its newest step.
	running    bool
	runStarted time.Time
	runStep    string
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
	case appsMsg:
		m.apps, m.offline = msg, nil
		m.appSelected = min(m.appSelected, max(len(m.apps)-1, 0))
		if m.detail != nil {
			return m, m.loadDetail(m.detail.App.Name)
		}
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
			m.input = dropLast(m.input)
		default:
			m.input += msg.Text
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
	case "q", "esc":
		switch {
		case m.added != nil:
			m.added = nil
		case m.detail != nil:
			m.detail, m.output, m.outputTitle, m.outputID, m.running = nil, nil, "", 0, false
		default:
			return m, tea.Quit
		}
		return m, nil
	case "tab":
		m.tab = (m.tab + 1) % 2
		m.added = nil
		return m, nil
	}
	if m.tab == tabServers {
		return m.serverKey(key)
	}
	return m.appKey(key)
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
				m.output, m.outputTitle, m.outputID, m.running = nil, "", 0, false
				return m, m.loadDetail(app.Name)
			}
			return m, nil
		}
	}
	app, ok := m.currentApp()
	switch key {
	case "d":
		return m, m.deploy()
	case "l":
		if ok {
			return m, m.logs(app.Name)
		}
	case "r":
		if ok {
			m.mode = modeConfirmRollback
		}
	case "R":
		if ok {
			return m, m.redeploy(app.Name)
		}
	case "e":
		if ok {
			m.mode, m.input = modeEnv, ""
			return m, m.showEnv(app.Name)
		}
	case "i":
		if ok {
			m.mode, m.input = modeImage, ""
		}
	case "h":
		if ok {
			return m, m.newHook(app.Name)
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

// showEnv fills the output panel with the env keys; values stay hidden.
func (m model) showEnv(name string) tea.Cmd {
	return func() tea.Msg {
		env, err := m.api.Env(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return m.envOutput(name, env)
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
		return m.envOutput(name, env)
	}
}

func (m model) envOutput(name string, env map[string]string) outputMsg {
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
	return outputMsg{id: m.shared.outputs.Add(1), title: "env of " + name, note: style.Muted.Render("values hidden · the next deploy gets changes"), lines: lines}
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
