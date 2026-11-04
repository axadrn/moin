package tui

import (
	"fmt"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
)

// metricsWidth is how many characters a sparkline takes.
const metricsWidth = 40

// metricsLoadedMsg is the metrics of an app or a server for the output
// panel. While the panel still shows them, they load again every 15
// seconds: the panel is live.
type metricsLoadedMsg struct {
	output      outputMsg
	app, server string
}

// metricsRefreshMsg loads the metrics in the panel again.
type metricsRefreshMsg struct {
	id          int64
	app, server string
}

// showMetrics shows the metrics of an app, or of a server when app is "".
func (m model) showMetrics(app, server string) tea.Cmd {
	return m.loadMetrics(0, app, server)
}

// loadMetrics loads the metrics for the panel id, 0 for a new panel.
func (m model) loadMetrics(id int64, app, server string) tea.Cmd {
	return func() tea.Msg {
		if id == 0 {
			id = m.shared.outputs.Add(1)
		}
		if app != "" {
			answer, err := m.api.AppMetrics(m.ctx, app, "")
			if err != nil {
				return errMsg{err}
			}
			return metricsLoadedMsg{output: appMetricsOutput(id, answer), app: app}
		}
		answer, err := m.api.ServerMetrics(m.ctx, server, "")
		if err != nil {
			return errMsg{err}
		}
		return metricsLoadedMsg{output: serverMetricsOutput(id, answer), server: server}
	}
}

// metricsUpdate shows loaded metrics and plans the next load; metrics an
// output shown later replaced stop.
func (m model) metricsUpdate(msg tea.Msg) (model, tea.Cmd) {
	switch msg := msg.(type) {
	case metricsLoadedMsg:
		if msg.output.id < m.outputID {
			return m, nil
		}
		refresh := msg.output.id == m.outputID
		m.outputID, m.outputTitle, m.outputNote, m.output = msg.output.id, msg.output.title, msg.output.note, msg.output.lines
		if !refresh {
			m.outputScroll, m.panelMatch, m.running = 0, 0, false
		}
		next := metricsRefreshMsg{id: msg.output.id, app: msg.app, server: msg.server}
		return m, tea.Tick(15*time.Second, func(time.Time) tea.Msg { return next })
	case metricsRefreshMsg:
		if msg.id != m.outputID {
			return m, nil
		}
		return m, m.loadMetrics(msg.id, msg.app, msg.server)
	}
	return m, nil
}

func spark(points []client.MetricsPoint, value func(client.MetricsPoint) float64) string {
	values := make([]float64, 0, len(points))
	for _, point := range points {
		values = append(values, value(point))
	}
	return style.Accent.Render(client.Sparkline(values, metricsWidth))
}

func cpuText(value float64) string { return strconv.FormatFloat(value, 'f', 1, 64) + "%" }

func rateText(value float64) string { return client.Bytes(value) + "/s" }

func appMetricsOutput(id int64, answer client.AppMetrics) outputMsg {
	title := "metrics of " + answer.App
	note := style.Muted.Render("the last hour, live every 15 seconds")
	if !answer.ServerOnline && answer.Server != "" {
		note = style.Danger.Render(style.SymbolFailed + " the server " + answer.Server + " is offline")
	}
	if answer.Now == nil && len(answer.Points) == 0 {
		return outputMsg{id: id, title: title, note: note, lines: []string{"no metrics yet: the agent sends them every 15 seconds while the app runs; update an older agent with u on the servers tab"}}
	}
	now := answer.Now
	cpu, memory, in, out := "-", "-", "-", "-"
	if now != nil {
		cpu, in, out = cpuText(now.CPU), rateText(now.NetRx), rateText(now.NetTx)
		memory = client.Bytes(float64(now.MemoryUsed))
		if now.MemoryLimit > 0 {
			memory += " of " + client.Bytes(float64(now.MemoryLimit))
		}
	}
	lines := resultLines([]string{"", "now", "last hour"}, [][]any{
		{"cpu", cpu, spark(answer.Points, func(p client.MetricsPoint) float64 { return p.CPU })},
		{"memory", memory, spark(answer.Points, func(p client.MetricsPoint) float64 { return float64(p.Memory) })},
		{"net in", in, spark(answer.Points, func(p client.MetricsPoint) float64 { return p.NetRx })},
		{"net out", out, spark(answer.Points, func(p client.MetricsPoint) float64 { return p.NetTx })},
	})
	for _, deploy := range answer.Deploys {
		lines = append(lines, style.Muted.Render(fmt.Sprintf("v%d went live %s UTC", deploy.Number, deploy.At.UTC().Format("15:04"))))
	}
	return outputMsg{id: id, title: title, note: note, lines: lines}
}

func serverMetricsOutput(id int64, answer client.ServerMetrics) outputMsg {
	title := "metrics of " + answer.Server
	note := style.Muted.Render("the last hour, live every 15 seconds")
	if !answer.Online {
		note = style.Danger.Render(style.SymbolFailed + " the server is offline")
	}
	if answer.Now == nil && len(answer.Points) == 0 {
		return outputMsg{id: id, title: title, note: note, lines: []string{"no metrics yet: the agent sends them every 15 seconds; update an older agent with u"}}
	}
	now := answer.Now
	cpu, memory, disk, in, out := "-", "-", "-", "-", "-"
	if now != nil {
		cpu = fmt.Sprintf("%s, %d cores, load %.2f", cpuText(now.CPU), now.Cores, now.Load)
		memory = client.Bytes(float64(now.MemoryUsed)) + " of " + client.Bytes(float64(now.MemoryTotal))
		disk = client.Bytes(float64(now.DiskUsed)) + " of " + client.Bytes(float64(now.DiskTotal)) + ", " + client.Share(float64(now.DiskUsed), float64(now.DiskTotal))
		in, out = rateText(now.NetRx), rateText(now.NetTx)
	}
	lines := resultLines([]string{"", "now", "last hour"}, [][]any{
		{"cpu", cpu, spark(answer.Points, func(p client.MetricsPoint) float64 { return p.CPU })},
		{"memory", memory, spark(answer.Points, func(p client.MetricsPoint) float64 { return float64(p.Memory) })},
		{"disk", disk, spark(answer.Points, func(p client.MetricsPoint) float64 { return float64(p.Disk) })},
		{"net in", in, spark(answer.Points, func(p client.MetricsPoint) float64 { return p.NetRx })},
		{"net out", out, spark(answer.Points, func(p client.MetricsPoint) float64 { return p.NetTx })},
	})
	if len(answer.Apps) > 0 {
		rows := [][]any{}
		for _, app := range answer.Apps {
			if app.Now == nil {
				rows = append(rows, []any{app.App, "-", "not running", "-"})
				continue
			}
			rows = append(rows, []any{app.App, cpuText(app.Now.CPU), client.Bytes(float64(app.Now.MemoryUsed)), rateText(app.Now.NetRx + app.Now.NetTx)})
		}
		lines = append(lines, "")
		lines = append(lines, resultLines([]string{"app", "cpu", "memory", "network"}, rows)...)
	}
	return outputMsg{id: id, title: title, note: note, lines: lines}
}
