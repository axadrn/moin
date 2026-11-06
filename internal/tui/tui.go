// Package tui is what `moin` opens without a command: full-screen views of
// the daily work, live from the control plane. It starts on the dashboard;
// `space` opens the menu, `space space` or `ctrl+k` searches, `esc` goes back.
//
// tui.go holds the state and the message loop, keys.go the keys, load.go
// the calls to the control plane, menu.go the menu and the search, and
// the view files draw it.
package tui

import (
	"context"
	"errors"
	"net"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/client"
	"github.com/charmbracelet/colorprofile"
)

// Options start the TUI.
type Options struct {
	// Config is where the control plane is and the token, as `moin login`
	// saved it. Without a token the TUI signs in first.
	Config client.Config
	// Dir is the directory the TUI started in, which `D` deploys.
	Dir string
	// Login signs in with the browser: it calls show with the code to type
	// and the page to type it on, waits until the user confirmed, and saves
	// the credentials where the CLI keeps them. Nil without one.
	Login func(ctx context.Context, show func(userCode, verificationURL string)) error
	// Browse opens a page in the browser; nil shows the address instead.
	Browse func(url string) error
}

// Run opens the TUI until the user quits or ctx ends.
func Run(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if opts.Browse != nil {
		openBrowser = opts.Browse
	}
	m := newModel(ctx, opts)
	if opts.Config.Token != "" {
		api, err := client.New(opts.Config)
		if err != nil {
			return err
		}
		m = m.connect(api, cacheKey(opts.Config.URL, opts.Config.Token)).restoreCache()
	}
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if last, ok := final.(model); ok {
		_ = last.saveCache() // a cache that cannot be written only costs the next start a moment
	}
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// view is a kind of full-screen view.
type view int

const (
	vDash view = iota
	vApp
	vDeploy
	vExec
	vServers
	vServer
	vActivity
	vWeb
)

// tab is a part of the app view, named by its key.
type tab byte

const (
	tDeploys tab = 'd'
	tLogs    tab = 'l'
	tBackups tab = 'b'
	tEnv     tab = 'e'
	tDomains tab = 'o'
	tUptime  tab = 'u'
	tMetrics tab = 'm'
)

var tabs = []struct {
	t    tab
	name string
}{{tDeploys, "deploys"}, {tLogs, "logs"}, {tBackups, "backups"}, {tEnv, "env"}, {tDomains, "domains"}, {tUptime, "uptime"}, {tMetrics, "metrics"}}

func tabName(t tab) string {
	for _, x := range tabs {
		if x.t == t {
			return x.name
		}
	}
	return ""
}

// screen is one place on the navigation stack.
type screen struct {
	v      view
	app    string
	tab    tab
	server string
	deploy int
}

// workspace is who and where the token is.
type workspace struct {
	slug, name, email, user string
	version                 string
	selfHosted              bool
}

// appData is what the open app shows. It belongs to one app and is
// dropped when another opens.
type appData struct {
	name    string
	detail  *client.AppDetail
	logs    []string
	logsAt  time.Time
	backups *client.AppBackups
	env     map[string]string
	domains []client.DomainCheck
	instant *client.Instant
	uptime  *client.Uptime
	metrics *client.AppMetrics
	// errs hold why a tab could not load, by tab.
	errs map[tab]string
}

// stream is output that comes in while you watch: a deploy log or a
// command. id drops lines of one you left.
type stream struct {
	id      int64
	app     string
	number  int
	title   string
	lines   []string
	running bool
	status  string // succeeded, failed, or a label like "exit 0"
	err     string
	deploy  *client.Deploy
}

// scroll is where a long text is: 0 follows the end, else the lines from
// the end the view stays above.
type scroll struct {
	back   int
	paused bool
	filter string
}

type model struct {
	ctx   context.Context
	api   *client.Client
	url   string
	dir   string
	hub   chan tea.Msg
	login func(ctx context.Context, show func(userCode, verificationURL string)) error

	w, h  int
	pal   palette
	frame int
	now   func() time.Time

	auth    *auth  // the sign-in view; nil once signed in
	cacheID string // names the cache of this control plane and login

	ws        workspace
	apps      []client.App
	servers   []client.Server
	loaded    bool // the first app list came
	cached    bool // what shows came from the cache of the last run
	live      bool // the status came from the control plane, not the cache
	wsLive    bool // the workspace came from the control plane
	status    *client.Status
	events    []client.Event
	eventsEnd string
	cpu       map[string][]float64
	usage     map[string]*client.ServerMetricsNow
	offline   error // the live stream broke

	cur   screen
	stack []screen
	sel   map[string]int
	focus int // the dashboard panel with the cursor

	app      appData
	seen     map[string]appData // what other apps showed when you left them
	srv      *client.ServerMetrics
	out      stream
	nextID   int64
	logs     scroll
	execLine string
	text     scroll // the deploy and exec output
	shown    bool   // env values visible
	rng      int    // metrics range, an index into ranges

	menu    []string // the which-key path; nil when closed
	menuOn  bool
	pick    *picker
	recent  []string // the titles of the last picks of the search, newest first
	help    bool
	helpTop int // the first line the keys layer shows
	prompt  *prompt
	ask     *confirm
	restore *restore

	notice    string
	noticeErr bool
	noticeAt  int
}

var ranges = []string{"1h", "24h", "7d", "30d"}

func newModel(ctx context.Context, opts Options) model {
	return model{
		ctx: ctx, url: opts.Config.URL, dir: opts.Dir, login: opts.Login,
		hub: make(chan tea.Msg, 256), w: 100, h: 30, pal: dark, now: time.Now,
		cur: screen{v: vDash}, sel: map[string]int{}, seen: map[string]appData{}, cpu: map[string][]float64{}, usage: map[string]*client.ServerMetricsNow{},
		auth: &auth{},
	}
}

// connect is the model signed in with api; key names its cache.
func (m model) connect(api *client.Client, key string) model {
	api.UseSurface("tui")
	m.api, m.cacheID = api, key
	m.auth = nil
	return m
}

type (
	tickMsg      time.Time
	hubMsg       struct{ msg tea.Msg }
	serversMsg   []client.Server
	appsMsg      []client.App
	offlineMsg   struct{ err error }
	statusMsg    client.Status
	workspaceMsg workspace
	eventsMsg    struct {
		page  client.ActivityPage
		older bool
	}
	usageMsg struct {
		server string
		m      client.ServerMetrics
	}
	serverMetricsMsg client.ServerMetrics
	detailMsg        client.AppDetail
	logsMsg          struct {
		app   string
		lines []string
	}
	backupsMsg struct {
		app string
		b   client.AppBackups
	}
	envMsg struct {
		app string
		env map[string]string
	}
	domainsMsg struct {
		app     string
		checks  []client.DomainCheck
		instant *client.Instant
	}
	uptimeMsg     client.Uptime
	appMetricsMsg client.AppMetrics
	// tabErrMsg is why a tab of an app could not load.
	tabErrMsg struct {
		app string
		tab tab
		err error
	}
	// startedMsg is a deploy an action started; the TUI follows it.
	startedMsg struct {
		app    string
		deploy client.Deploy
		title  string
	}
	deployMsg client.Deploy
	lineMsg   struct {
		id   int64
		line string
	}
	doneMsg struct {
		id     int64
		status string
		err    error
		notice string
		deploy *client.Deploy
	}
	noticeMsg string
	errMsg    struct{ err error }
	// undoMsg is a change the view showed at once that the control plane
	// refused: it says why and loads the truth again.
	undoMsg struct{ err error }
	// optimisticMsg shows a change at once, then sends it; the remove of
	// an env var after its question.
	optimisticMsg struct {
		do  func(m model) model
		cmd tea.Cmd
	}
	// reloadMsg loads the open tab again after a change.
	reloadMsg  struct{ notice string }
	stoppedMsg string
	pinnedMsg  struct {
		app string
		on  bool
	}
)

func tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// listen waits for the next message the background streams sent.
func (m model) listen() tea.Cmd {
	hub := m.hub
	return func() tea.Msg { return hubMsg{<-hub} }
}

// send hands a message from a goroutine to the loop.
func (m model) send(msg tea.Msg) {
	select {
	case m.hub <- msg:
	case <-m.ctx.Done():
	}
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick(), m.listen(), tea.RequestBackgroundColor}
	if m.auth != nil {
		cmds = append(cmds, m.startLogin())
	} else {
		cmds = append(cmds, m.start())
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case hubMsg:
		next, cmd := m.Update(msg.msg)
		return next, tea.Batch(cmd, next.(model).listen())
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case tea.BackgroundColorMsg:
		mono := m.pal.mono
		m.pal = dark
		if !msg.IsDark() {
			m.pal = light
		}
		m.pal.mono = mono
		return m, nil
	case tea.ColorProfileMsg:
		// NO_COLOR, or a terminal without colour
		m.pal.mono = msg.Profile == colorprofile.ASCII || msg.Profile == colorprofile.NoTTY
		return m, nil
	case tickMsg:
		return m.tick()
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseClickMsg, tea.MouseWheelMsg:
		return m.mouse(msg.(tea.MouseMsg))
	case tea.PasteMsg:
		return m.paste(msg.Content), nil
	}
	if m.auth != nil {
		return m.authUpdate(msg)
	}
	return m.data(msg)
}

// tick moves the pulse and reloads what the open view shows live.
func (m model) tick() (tea.Model, tea.Cmd) {
	m.frame++
	cmds := []tea.Cmd{tick()}
	// a result stays 8 seconds, an error 20, to read it
	life := 16
	if m.noticeErr {
		life = 40
	}
	if m.notice != "" && m.frame-m.noticeAt > life {
		m.notice = ""
	}
	if m.api == nil {
		return m, tea.Batch(cmds...)
	}
	every := func(n int) bool { return m.frame%n == 0 }
	if every(60) {
		cmds = append(cmds, m.loadStatus(), m.loadEvents(""))
	}
	if every(30) && m.cur.v == vDash {
		cmds = append(cmds, m.loadUsage()...)
	}
	switch m.cur.v {
	case vApp:
		switch m.cur.tab {
		case tLogs:
			if every(6) && !m.logs.paused {
				cmds = append(cmds, m.loadLogs(m.cur.app))
			}
		case tMetrics:
			if every(30) {
				cmds = append(cmds, m.loadAppMetrics(m.cur.app))
			}
		case tUptime:
			if every(60) {
				cmds = append(cmds, m.loadUptime(m.cur.app))
			}
		}
	case vServer:
		if every(30) {
			cmds = append(cmds, m.loadServerMetrics(m.cur.server))
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *model) say(s string) {
	m.notice, m.noticeErr, m.noticeAt = s, false, m.frame
}

func (m *model) fail(err error) {
	if err == nil {
		return
	}
	m.notice, m.noticeErr, m.noticeAt = reason(err), true, m.frame
}

// reason is an error as a sentence; a control plane that does not answer
// says so instead of the dial error.
func reason(err error) string {
	var op *net.OpError
	if errors.As(err, &op) {
		return "The control plane does not answer. This loads again when it is back."
	}
	return err.Error()
}

// data takes what the control plane answered.
func (m model) data(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case serversMsg:
		m.servers, m.offline = msg, nil
	case appsMsg:
		first, back := !m.loaded, m.offline != nil
		m.apps, m.loaded, m.offline, m.cached = msg, true, nil, false
		cmds := []tea.Cmd{}
		if first {
			cmds = append(cmds, m.loadUsage()...)
		}
		// after a start or a break, what loaded once may have failed
		if !m.wsLive || back {
			cmds = append(cmds, m.loadWorkspace(), m.loadStatus(), m.loadEvents(""))
		}
		if back && len(m.app.errs) > 0 {
			cmds = append(cmds, m.openTab())
		}
		return m, tea.Batch(cmds...)
	case offlineMsg:
		m.offline = msg.err
	case statusMsg:
		s := client.Status(msg)
		m.status, m.live = &s, true
	case workspaceMsg:
		m.ws, m.wsLive = workspace(msg), true
	case eventsMsg:
		if msg.older {
			m.events = append(m.events, msg.page.Items...)
		} else {
			m.events = msg.page.Items
		}
		m.eventsEnd = msg.page.Next
	case usageMsg:
		var cpu []float64
		for _, p := range msg.m.Points {
			cpu = append(cpu, p.CPU)
		}
		m.cpu[msg.server] = cpu
		m.usage[msg.server] = msg.m.Now
	case serverMetricsMsg:
		s := client.ServerMetrics(msg)
		if s.Server == m.cur.server {
			m.srv = &s
		}
	case detailMsg:
		if msg.App.Name == m.app.name {
			d := client.AppDetail(msg)
			m.app.detail = &d
		}
	case logsMsg:
		if msg.app == m.app.name {
			m.app.logs, m.app.logsAt = msg.lines, m.now()
			delete(m.app.errs, tLogs)
		}
	case backupsMsg:
		if msg.app == m.app.name {
			m.app.backups = &msg.b
			delete(m.app.errs, tBackups)
		}
	case envMsg:
		if msg.app == m.app.name {
			m.app.env = msg.env
			delete(m.app.errs, tEnv)
		}
	case domainsMsg:
		if msg.app == m.app.name {
			m.app.domains, m.app.instant = msg.checks, msg.instant
			delete(m.app.errs, tDomains)
		}
	case uptimeMsg:
		if msg.App == m.app.name {
			u := client.Uptime(msg)
			m.app.uptime = &u
			delete(m.app.errs, tUptime)
		}
	case appMetricsMsg:
		if msg.App == m.app.name {
			a := client.AppMetrics(msg)
			m.app.metrics = &a
			delete(m.app.errs, tMetrics)
		}
	case tabErrMsg:
		if msg.app == m.app.name {
			if m.app.errs == nil {
				m.app.errs = map[tab]string{}
			}
			m.app.errs[msg.tab] = reason(msg.err)
		}
	case startedMsg:
		return m.follow(msg)
	case deployMsg:
		d := client.Deploy(msg)
		if m.cur.v == vDeploy && m.out.number == d.Number && !m.out.running {
			m.out.deploy = &d
			m.out.lines = d.Log
			m.out.status = d.Status
			m.out.err = d.Error
		}
	case lineMsg:
		if msg.id == m.out.id {
			m.out.lines = append(m.out.lines, msg.line)
		}
	case doneMsg:
		if msg.id == m.out.id {
			m.out.running = false
			m.out.status = msg.status
			if msg.deploy != nil {
				m.out.deploy = msg.deploy
			}
			if msg.err != nil {
				m.out.err = msg.err.Error()
			}
		}
		if msg.err != nil {
			m.fail(msg.err)
		} else if msg.notice != "" {
			m.say(msg.notice)
		}
		if m.out.app != "" {
			return m, m.loadDetail(m.out.app)
		}
	case pinnedMsg:
		for i := range m.apps {
			if m.apps[i].Name == msg.app {
				m.apps[i].Pinned = msg.on
			}
		}
		if msg.on {
			m.say(msg.app + " is pinned. * unpins it")
		} else {
			m.say(msg.app + " is unpinned")
		}
	case stoppedMsg:
		for i := range m.apps {
			if m.apps[i].Name == string(msg) {
				m.apps[i].Stopped = true
			}
		}
		m.say(string(msg) + " is stopped. S starts it again")
	case reloadMsg:
		if msg.notice != "" {
			m.say(msg.notice)
		}
		return m, m.openTab()
	case noticeMsg:
		m.say(string(msg))
	case errMsg:
		m.fail(msg.err)
	case undoMsg:
		m.fail(msg.err)
		return m, m.openTab()
	case optimisticMsg:
		return msg.do(m), msg.cmd
	case editMsg:
		return m, m.openEditor(msg)
	case editedMsg:
		return m.edited(msg)
	}
	return m, nil
}

// open goes one level deeper.
func (m model) open(s screen) (model, tea.Cmd) {
	if s == m.cur {
		return m, nil
	}
	m.stack = append(m.stack, m.cur)
	return m.show(s)
}

// show makes s the current screen and loads what it needs.
func (m model) show(s screen) (model, tea.Cmd) {
	if s.app != "" && s.app != m.app.name {
		// an app you come back to shows what it showed at once, and loads
		// again behind it
		if m.app.name != "" {
			m.seen[m.app.name] = m.app
		}
		m.app = appData{name: s.app}
		if seen, ok := m.seen[s.app]; ok {
			seen.errs = nil // they load again
			m.app = seen
		}
		m.logs = scroll{}
		m.shown = false
	}
	if s.v == vApp && s.tab == 0 {
		s.tab = tDeploys
	}
	if s.v != m.cur.v || s.server != m.cur.server {
		m.srv = nil
	}
	m.cur = s
	return m, m.enter()
}

// back goes one level up; on the dashboard it stays.
func (m model) back() (model, tea.Cmd) {
	if len(m.stack) == 0 {
		if m.cur.v == vDash {
			return m, nil
		}
		return m.show(screen{v: vDash})
	}
	s := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return m.show(s)
}

// home empties the stack.
func (m model) home() (model, tea.Cmd) {
	m.stack = nil
	return m.show(screen{v: vDash})
}

// openApp opens an app at a tab; inside the same app it only switches.
func (m model) openApp(name string, t tab) (model, tea.Cmd) {
	if m.cur.v == vApp && m.cur.app == name {
		return m.switchTab(t)
	}
	return m.open(screen{v: vApp, app: name, tab: t})
}

func (m model) switchTab(t tab) (model, tea.Cmd) {
	if m.cur.tab == t {
		return m, nil
	}
	m.cur.tab = t
	return m, m.openTab()
}

// enter loads what the current screen shows.
func (m model) enter() tea.Cmd {
	if m.api == nil {
		return nil
	}
	switch m.cur.v {
	case vDash:
		return tea.Batch(append(m.loadUsage(), m.loadStatus(), m.loadEvents(""))...)
	case vApp:
		return tea.Batch(m.loadDetail(m.cur.app), m.openTab())
	case vDeploy:
		if m.out.running && m.out.number == m.cur.deploy && m.out.app == m.cur.app {
			return nil
		}
		return m.loadDeploy(m.cur.app, m.cur.deploy)
	case vServer:
		return m.loadServerMetrics(m.cur.server)
	case vActivity:
		return m.loadEvents("")
	}
	return nil
}

// openTab loads the open tab of the app.
func (m model) openTab() tea.Cmd {
	if m.api == nil || m.cur.v != vApp {
		return nil
	}
	a := m.cur.app
	switch m.cur.tab {
	case tDeploys:
		return m.loadDetail(a)
	case tLogs:
		return m.loadLogs(a)
	case tBackups:
		return m.loadBackups(a)
	case tEnv:
		return m.loadEnv(a)
	case tDomains:
		return m.loadDomains(a)
	case tUptime:
		return m.loadUptime(a)
	case tMetrics:
		return m.loadAppMetrics(a)
	}
	return nil
}

// follow opens the deploy an action started and streams its log.
func (m model) follow(msg startedMsg) (tea.Model, tea.Cmd) {
	m.nextID++
	m.out = stream{id: m.nextID, app: msg.app, number: msg.deploy.Number, title: msg.title, running: true, status: "running"}
	m.text = scroll{}
	m, cmd := m.open(screen{v: vDeploy, app: msg.app, deploy: msg.deploy.Number})
	return m, tea.Batch(cmd, m.followDeploy(m.out.id, msg.app, msg.deploy.Number))
}

// findApp is the app by name from the live list.
func (m model) findApp(name string) (client.App, bool) {
	for _, a := range m.apps {
		if a.Name == name {
			return a, true
		}
	}
	return client.App{}, false
}

func (m model) findServer(name string) (client.Server, bool) {
	for _, s := range m.servers {
		if s.Name == name {
			return s, true
		}
	}
	return client.Server{}, false
}

// cursor moves the cursor of a list of n by d and keeps it inside.
func (m model) cursor(name string, n, d int) model {
	i := m.sel[name] + d
	i = max(0, min(i, n-1))
	m.sel[name] = i
	return m
}

func (m model) at(name string, n int) int {
	return max(0, min(m.sel[name], n-1))
}
