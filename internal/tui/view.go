package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
	"github.com/axadrn/moin/protocol"
)

// View draws the screen: the header, the body, and at the bottom the
// line of the last result and the keys.
func (m *model) View() tea.View {
	footer := []string{m.statusLine(), style.Muted.Render(m.keys())}
	rows := max(m.height-len(footer)-2, 1)
	var body []string
	switch {
	case m.help:
		body = helpLines()
	case m.picker != nil:
		body = m.pickerLines()
	case m.app != nil:
		body = m.appLines(rows)
	default:
		body = m.listLines(rows)
	}
	if len(body) > rows {
		body = body[:rows]
	}
	for len(body) < rows {
		body = append(body, "")
	}
	lines := append([]string{m.header(), ""}, body...)
	lines = append(lines, footer...)
	for index, line := range lines {
		lines[index] = fit(line, m.width)
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	return view
}

// header is moin's name, and on an app screen the app and its state.
func (m *model) header() string {
	name := style.Accent.Render(build.Name)
	if m.app == nil {
		return name
	}
	key := m.app.key
	line := name + "  " + style.Bold.Render(key.app) + style.Muted.Render(" on "+key.server)
	if app, ok := m.appStatus(key); ok {
		line += "  " + releaseName(app.Release) + "  " + appState(app) + "  " + strings.Join(app.Addresses, " ")
	}
	return line
}

// listLines are every server and its apps. When they do not fit, the
// list scrolls so the selected app stays in view.
func (m *model) listLines(rows int) []string {
	nameWidth, releaseWidth := 0, 0
	for _, server := range m.servers {
		for _, app := range server.status.Apps {
			nameWidth = max(nameWidth, len(app.Name))
			releaseWidth = max(releaseWidth, len(releaseName(app.Release)))
		}
	}
	var lines []string
	selectedLine := 0
	for _, server := range m.servers {
		name := style.Bold.Render(server.name)
		switch {
		case !server.answered:
			lines = append(lines, name+"  "+style.Badge(style.StatusRunning, "asking"))
			continue
		case server.err != nil:
			lines = append(lines, name+"  "+style.Badge(style.StatusFailed, "unreachable")+"  "+style.Muted.Render(m.cli.Explain(server.err)))
			continue
		}
		if server.status.KnocksToday > 0 {
			name += "  " + style.Muted.Render(human.Knocks(server.status.KnocksToday))
		}
		lines = append(lines, name)
		if server.status.PublicSSH {
			lines = append(lines, "    "+style.Danger.Render("port 22 open to the internet"))
		}
		if len(server.status.Apps) == 0 {
			lines = append(lines, "    "+style.Muted.Render("no apps"))
		}
		for _, app := range server.status.Apps {
			marker := "  "
			appName := pad(app.Name, nameWidth)
			if (appKey{server.name, app.Name}) == m.selected {
				marker = style.Bold.Render(style.SymbolSelected) + " "
				appName = style.Bold.Render(appName)
				selectedLine = len(lines)
			}
			lines = append(lines, "  "+marker+appName+"   "+pad(releaseName(app.Release), releaseWidth)+"   "+appState(app)+"   "+style.Muted.Render(strings.Join(app.Addresses, " ")))
		}
	}
	if selectedLine >= rows {
		lines = lines[selectedLine-rows+1:]
	}
	return lines
}

// appLines are the app's backups and below them its logs, as many of
// the newest lines as fit.
func (m *model) appLines(rows int) []string {
	app := m.app
	lines := []string{style.Bold.Render("Backups")}
	switch {
	case !app.backupsLoaded:
		lines = append(lines, "  "+style.Badge(style.StatusRunning, "asking"))
	case app.backupsError != "":
		lines = append(lines, "  "+failed(app.backupsError))
	case len(app.backups) == 0:
		lines = append(lines, "  "+style.Muted.Render("No database moin can copy: no SQLite file on a volume, no Postgres service."))
	}
	for _, database := range app.backups {
		lines = append(lines, "  "+backupLine(database, time.Now()))
	}
	state := style.Muted.Render("following")
	if app.logsEnded != "" {
		state = style.Muted.Render(app.logsEnded)
	}
	lines = append(lines, "", style.Bold.Render("Logs")+"  "+state)
	room := max(rows-len(lines), 0)
	logs := app.logs[max(len(app.logs)-room, 0):]
	for _, line := range logs {
		lines = append(lines, "  "+line)
	}
	return lines
}

// backupLine is one database in a line: its last check and its copies.
func backupLine(database protocol.DatabaseBackup, now time.Time) string {
	name := style.Bold.Render(database.Database) + "  "
	if database.Error != "" {
		return name + failed(database.Error)
	}
	check := style.Muted.Render("no check yet")
	switch {
	case database.CheckedAt != nil && database.CheckError != "":
		check = failed("check " + human.Ago(*database.CheckedAt, now) + ": " + database.CheckError)
	case database.CheckedAt != nil:
		check = succeeded("checked " + human.Ago(*database.CheckedAt, now))
	}
	copies := style.Muted.Render("no copy yet")
	if len(database.Copies) > 0 {
		copies = fmt.Sprintf("%d copies, newest %s", len(database.Copies), human.Ago(database.Copies[0].At, now))
	}
	return name + check + style.Muted.Render("  ·  ") + copies
}

// pickerLines are the releases to go back to, the picked one marked.
func (m *model) pickerLines() []string {
	p := m.picker
	lines := []string{style.Bold.Render("Go back to which release of " + p.key.app + "?")}
	now := time.Now()
	for index, release := range p.releases {
		marker := "  "
		number := releaseName(release.Number)
		if index == p.cursor {
			marker = style.Bold.Render(style.SymbolSelected) + " "
			number = style.Bold.Render(number)
		}
		note := ""
		switch {
		case release.Live:
			note = style.Success.Render("live")
		case release.Before:
			note = style.Muted.Render("the one before")
		}
		lines = append(lines, "  "+marker+pad(number, 5)+"  "+pad(human.Ago(release.At, now), 12)+"  "+note)
	}
	return lines
}

// statusLine is the open question, what runs now, or the last result.
func (m *model) statusLine() string {
	switch {
	case m.question != nil:
		return style.Warning.Render("?") + " " + m.question.text + " " + style.Muted.Render("[y/N]")
	case m.busy != "":
		return style.Warning.Render(style.SymbolRunning) + " " + m.busy + style.Muted.Render(" …")
	}
	return m.message
}

// keys are the keys that work now.
func (m *model) keys() string {
	switch {
	case m.question != nil:
		return "y yes · any other key no"
	case m.picker != nil:
		return "↑↓ select · enter go back · esc cancel"
	case m.help:
		return "any key closes the help"
	case m.app != nil:
		return "esc back · r restart · b rollback · o open · ? help · q quit"
	}
	return "↑↓ select · enter logs and backups · r restart · b rollback · o open · ? help · q quit"
}

// helpLines say what each key does.
func helpLines() []string {
	keys := [][2]string{
		{"↑ ↓", "select an app"},
		{"enter", "its logs, following, and its backups"},
		{"esc", "back to every app"},
		{"r", "restart the app, after you say y"},
		{"b", "go back to an older release, after you say y"},
		{"o", "open the app's address in your browser"},
		{"?", "this help"},
		{"q", "quit"},
	}
	lines := []string{style.Bold.Render("Keys")}
	for _, key := range keys {
		lines = append(lines, "  "+pad(key[0], 7)+style.Muted.Render(key[1]))
	}
	lines = append(lines, "",
		style.Muted.Render("Every server is asked again every "+human.Duration(refreshEvery)+"."),
		style.Muted.Render("Deploy from the app's folder: ")+style.Accent.Render(build.Name+" deploy"),
		style.Muted.Render("Everything else: ")+style.Accent.Render(build.Name+" help"))
	return lines
}

// appState is how an app runs, as in the CLI's table.
func appState(app daemon.AppStatus) string {
	if app.Running {
		return style.Badge(style.StatusLive, "running")
	}
	return style.Badge(style.StatusIdle, "stopped")
}

// pad fills text with spaces to width cells.
func pad(text string, width int) string {
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}

// fit cuts a line to the screen's width, so it never wraps.
func fit(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}
