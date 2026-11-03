package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// showUptime shows the uptime of every domain of an app in the output
// panel, with the last outages.
func (m model) showUptime(name string) tea.Cmd {
	return func() tea.Msg {
		answer, err := m.api.Uptime(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return uptimeOutput(m.shared.outputs.Add(1), answer, time.Now())
	}
}

func uptimeOutput(id int64, answer client.Uptime, now time.Time) outputMsg {
	if len(answer.Domains) == 0 {
		return outputMsg{id: id, title: "uptime of " + answer.App, lines: []string{"no domains to check: add one with o"}}
	}
	rows := [][]any{}
	var outages []string
	for _, domain := range answer.Domains {
		check := "-"
		if domain.CheckedAt != nil {
			check = strconv.Itoa(domain.StatusCode) + " in " + strconv.Itoa(domain.ResponseMS) + " ms"
			if domain.Error != "" {
				check = domain.Error
			}
		}
		rows = append(rows, []any{domain.Domain, domain.State, client.Percent(domain.Uptime24h), client.Percent(domain.Uptime7d), client.Percent(domain.Uptime30d), check, domain.Path})
		for _, outage := range domain.Outages {
			lasted := "ongoing for " + duration(now.Sub(outage.StartedAt))
			if outage.EndedAt != nil {
				lasted = "down for " + duration(outage.EndedAt.Sub(outage.StartedAt))
			}
			outages = append(outages, outage.StartedAt.UTC().Format("2006-01-02 15:04")+"  "+domain.Domain+"  "+lasted+"  "+outage.Reason)
		}
	}
	lines := resultLines([]string{"domain", "state", "24h", "7d", "30d", "newest check", "path"}, rows)
	if len(outages) > 0 {
		lines = append(lines, "", style.Bold.Render("last outages"))
		lines = append(lines, outages...)
	}
	note := style.Muted.Render("checked every minute, like a visitor")
	if !answer.ServerOnline && answer.Server != "" {
		note = style.Danger.Render(style.SymbolFailed + " the server " + answer.Server + " is offline")
	}
	return outputMsg{id: id, title: "uptime of " + answer.App + ": " + answer.State, note: note, lines: lines}
}

// changeUptime reads what was typed at the uptime prompt: "pause blog.de",
// "resume blog.de", or "blog.de /health" and "blog.de default" for the
// path; then it shows the uptime again.
func (m model) changeUptime(name, input string) tea.Cmd {
	fields := strings.Fields(input)
	var path *string
	var paused *bool
	domain := ""
	switch {
	case len(fields) == 2 && (fields[0] == "pause" || fields[0] == "resume"):
		domain = fields[1]
		value := fields[0] == "pause"
		paused = &value
	case len(fields) == 2:
		domain = fields[0]
		value := fields[1]
		if value == "default" {
			value = ""
		}
		path = &value
	default:
		return func() tea.Msg {
			return errMsg{fmt.Errorf("type pause <domain>, resume <domain>, or <domain> /path")}
		}
	}
	return func() tea.Msg {
		answer, err := m.api.SetUptimeCheck(m.ctx, name, domain, path, paused)
		if err != nil {
			return errMsg{err}
		}
		notice := "the check of " + answer.Domain + " goes to " + answer.Path
		switch {
		case paused != nil && *paused:
			notice = "paused the check of " + answer.Domain
		case paused != nil:
			notice = "checking " + answer.Domain + " every minute again"
		}
		m.shared.program.Send(noticeMsg(notice))
		uptime, err := m.api.Uptime(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return uptimeOutput(m.shared.outputs.Add(1), uptime, time.Now())
	}
}
