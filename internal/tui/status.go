package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// showStatus runs :status: whether everything is OK, in the output panel.
func (m model) showStatus() tea.Cmd {
	return func() tea.Msg {
		status, err := m.api.Status(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return statusOutput(m.shared.outputs.Add(1), status)
	}
}

// statusOutput is what needs you, the updates and the counts, as panel
// lines.
func statusOutput(id int64, status client.Status) outputMsg {
	counts := fmt.Sprintf("%d of %d apps live · %d of %d servers online · %d of %d backups checked today",
		status.Counts.AppsLive, status.Counts.Apps, status.Counts.ServersOnline, status.Counts.Servers, status.Counts.BackupsChecked, status.Counts.Databases)
	var lines []string
	if status.OK {
		lines = append(lines, style.Success.Render(style.SymbolDone+" All good."))
	} else {
		for _, issue := range status.Issues {
			mark := style.Danger.Render(style.SymbolFailed)
			if issue.Kind == "agent.behind" {
				mark = style.Warning.Render(style.SymbolRunning)
			}
			lines = append(lines, mark+" "+issue.Target+"  "+style.Muted.Render(issue.Detail))
		}
	}
	lines = append(lines, style.Muted.Render(counts))
	if len(status.Updates) > 0 {
		lines = append(lines, "", "Updates")
		for _, update := range status.Updates {
			lines = append(lines, update.App+"  "+style.Muted.Render(update.Name)+" "+update.Version+" → "+update.Latest)
		}
	}
	title := "status: all good"
	if !status.OK {
		title = fmt.Sprintf("status: %d need you", len(status.Issues))
	}
	return outputMsg{id: id, title: title, note: style.Muted.Render(":status again to refresh"), lines: lines}
}
