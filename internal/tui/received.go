package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/client"
)

// received takes what the control plane answered: what a load brought,
// how a change went, and the lines of a running deploy.
func (m model) received(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case serversMsg:
		m.servers, m.offline = msg, nil
	case appsMsg:
		return m.gotApps(msg)
	case offlineMsg:
		m.offline = msg.err
	case statusMsg:
		return m.gotStatus(msg), nil
	case workspaceMsg:
		m.ws, m.wsLive = workspace(msg), true
	case eventsMsg:
		return m.gotEvents(msg), nil
	case usageMsg:
		return m.gotUsage(msg), nil
	case serverMetricsMsg:
		return m.gotServerMetrics(msg), nil
	case detailMsg:
		return m.gotDetail(msg), nil
	case logsMsg:
		return m.gotLogs(msg), nil
	case backupsMsg:
		return m.gotBackups(msg), nil
	case envMsg:
		return m.gotEnv(msg), nil
	case domainsMsg:
		return m.gotDomains(msg), nil
	case uptimeMsg:
		return m.gotUptime(msg), nil
	case appMetricsMsg:
		return m.gotAppMetrics(msg), nil
	case tabErrMsg:
		return m.gotTabErr(msg), nil
	case startedMsg:
		return m.follow(msg)
	case deployMsg:
		return m.gotDeploy(msg), nil
	case lineMsg:
		return m.gotLine(msg), nil
	case doneMsg:
		return m.gotDone(msg)
	case pinnedMsg:
		return m.gotPinned(msg), nil
	case stoppedMsg:
		return m.gotStopped(msg), nil
	case reloadMsg:
		return m.gotReload(msg)
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

// gotApps keeps the apps. The first list loads the usage of the servers.
func (m model) gotApps(msg appsMsg) (tea.Model, tea.Cmd) {
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
}

func (m model) gotStatus(msg statusMsg) model {
	s := client.Status(msg)
	m.status, m.live = &s, true
	return m
}

// gotEvents keeps a page of the activity: the newest, or an older one
// after those it has.
func (m model) gotEvents(msg eventsMsg) model {
	if msg.older {
		m.events = append(m.events, msg.page.Items...)
	} else {
		m.events = msg.page.Items
	}
	m.eventsEnd = msg.page.Next
	return m
}

// gotUsage keeps the CPU line and the current numbers of one server.
func (m model) gotUsage(msg usageMsg) model {
	var cpu []float64
	for _, p := range msg.m.Points {
		cpu = append(cpu, p.CPU)
	}
	m.cpu[msg.server] = cpu
	m.usage[msg.server] = msg.m.Now
	return m
}

// The answers for an app or a server count only while it is still open.

func (m model) gotServerMetrics(msg serverMetricsMsg) model {
	s := client.ServerMetrics(msg)
	if s.Server == m.cur.server {
		m.srv = &s
	}
	return m
}

func (m model) gotDetail(msg detailMsg) model {
	if msg.App.Name == m.app.name {
		d := client.AppDetail(msg)
		m.app.detail = &d
	}
	return m
}

func (m model) gotLogs(msg logsMsg) model {
	if msg.app == m.app.name {
		m.app.logs, m.app.logsAt = msg.lines, m.now()
		delete(m.app.errs, tLogs)
	}
	return m
}

func (m model) gotBackups(msg backupsMsg) model {
	if msg.app == m.app.name {
		m.app.backups = &msg.b
		delete(m.app.errs, tBackups)
	}
	return m
}

func (m model) gotEnv(msg envMsg) model {
	if msg.app == m.app.name {
		m.app.env = msg.env
		delete(m.app.errs, tEnv)
	}
	return m
}

func (m model) gotDomains(msg domainsMsg) model {
	if msg.app == m.app.name {
		m.app.domains, m.app.instant = msg.checks, msg.instant
		delete(m.app.errs, tDomains)
	}
	return m
}

func (m model) gotUptime(msg uptimeMsg) model {
	if msg.App == m.app.name {
		u := client.Uptime(msg)
		m.app.uptime = &u
		delete(m.app.errs, tUptime)
	}
	return m
}

func (m model) gotAppMetrics(msg appMetricsMsg) model {
	if msg.App == m.app.name {
		a := client.AppMetrics(msg)
		m.app.metrics = &a
		delete(m.app.errs, tMetrics)
	}
	return m
}

// gotTabErr keeps why a tab of the open app did not load, for the tab to
// show.
func (m model) gotTabErr(msg tabErrMsg) model {
	if msg.app == m.app.name {
		if m.app.errs == nil {
			m.app.errs = map[tab]string{}
		}
		m.app.errs[msg.tab] = reason(msg.err)
	}
	return m
}

// gotDeploy shows a finished deploy the deploy view has open.
func (m model) gotDeploy(msg deployMsg) model {
	d := client.Deploy(msg)
	if m.cur.v == vDeploy && m.out.number == d.Number && !m.out.running {
		m.out.deploy = &d
		m.out.lines = d.Log
		m.out.status = d.Status
		m.out.err = d.Error
	}
	return m
}

// gotLine adds a line to the output it follows.
func (m model) gotLine(msg lineMsg) model {
	if msg.id == m.out.id {
		m.out.lines = append(m.out.lines, msg.line)
	}
	return m
}

// gotDone ends the output it follows, says how it went and loads the app
// again.
func (m model) gotDone(msg doneMsg) (tea.Model, tea.Cmd) {
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
	return m, nil
}

func (m model) gotPinned(msg pinnedMsg) model {
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
	return m
}

func (m model) gotStopped(msg stoppedMsg) model {
	for i := range m.apps {
		if m.apps[i].Name == string(msg) {
			m.apps[i].Stopped = true
		}
	}
	m.say(string(msg) + " is stopped. S starts it again")
	return m
}

// gotReload says what changed and loads the open tab again.
func (m model) gotReload(msg reloadMsg) (tea.Model, tea.Cmd) {
	if msg.notice != "" {
		m.say(msg.notice)
	}
	return m, m.openTab()
}
