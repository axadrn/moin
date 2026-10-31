package tui

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// The screen: a header with the brand mark, the tabs and the control plane,
// the body, and a footer with notices, prompts and keys. The footer always
// shows; the body gets the rows that are left.

// pad indents body text past the selection gutter.
const pad = "   "

func (m model) View() tea.View {
	width := m.width
	if width <= 0 {
		width = 100
	}
	header := m.header(width)
	footer := m.footer(width)
	room := -1
	if m.height > 0 {
		room = max(m.height-len(header)-len(footer)-1, 0)
	}
	body := m.body(width, room)
	if room >= 0 {
		body = body[:min(len(body), room)]
		for len(body) < room {
			body = append(body, "")
		}
	}
	lines := append(append(append(header, ""), body...), footer...)
	// Too small for even the header and footer: the footer wins.
	if m.height > 0 && len(lines) > m.height {
		lines = lines[len(lines)-m.height:]
	}
	for index, line := range lines {
		lines[index] = fit(line, width)
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	return view
}

func (m model) header(width int) []string {
	tab := func(name string, active bool, count int) string {
		label := style.Muted.Render(name)
		if active {
			label = style.Accent.Bold(true).Render(name)
		}
		return label + style.Dim.Render(fmt.Sprintf(" %d", count))
	}
	servers := tab("servers", m.tab == tabServers, len(m.servers))
	apps := tab("apps", m.tab == tabApps, len(m.apps))
	activity := style.Muted.Render("activity")
	if m.tab == tabActivity {
		activity = style.Accent.Bold(true).Render("activity")
	}
	if m.tab == tabApps && m.detail != nil {
		apps = style.Accent.Bold(true).Render("apps") + style.Muted.Render(" › ") + style.Bold.Render(m.detail.App.Name)
	}
	left := " " + style.Mark.Render(build.Name) + "   " + servers + "   " + apps + "   " + activity

	right := style.Muted.Render(host(m.url))
	switch {
	case m.offline != nil:
		right = style.Danger.Render(style.SymbolFailed+" reconnecting") + "  " + right
	case m.loading:
		right = style.Warning.Render(style.SymbolRunning+" connecting") + "  " + right
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 2 {
		return []string{left, rule(width)}
	}
	return []string{left + strings.Repeat(" ", gap) + right, rule(width)}
}

// host is the control plane without the scheme, like "ops.goilerplate.com".
func host(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host + strings.TrimRight(parsed.Path, "/")
}

func rule(width int) string {
	return style.Dim.Render(strings.Repeat("─", max(width, 0)))
}

func (m model) body(width, room int) []string {
	switch {
	case m.tab == tabServers:
		return m.serversView(width, room)
	case m.tab == tabActivity:
		return m.activityView(room)
	case m.detail != nil:
		return m.detailView(width, room)
	}
	return m.appsView(room)
}

// activityView is who did what, newest first, as many as fit.
func (m model) activityView(room int) []string {
	if len(m.events) == 0 {
		return []string{pad + style.Muted.Render("Nothing happened yet. Deploys, restores, SQL writes and every other change show up here.")}
	}
	rows := make([][]string, 0, len(m.events))
	for _, event := range m.events {
		rows = append(rows, []string{style.Muted.Render(event.CreatedAt.Local().Format("Jan 2 15:04")), event.Actor, style.Muted.Render(viaName(event.Via)), event.Summary})
	}
	return table([]string{"WHEN", "WHO", "VIA", "WHAT"}, rows, -1, room)
}

// viaName names how a change came in.
func viaName(via string) string {
	switch via {
	case "api":
		return "cli"
	case "mcp":
		return "agent"
	case "hook":
		return "deploy hook"
	}
	return via
}

func (m model) serversView(width, room int) []string {
	var added []string
	if m.added != nil {
		added = append(added, "",
			pad+style.Success.Render(style.SymbolDone)+" "+style.Bold.Render(m.added.Server.Name)+" added. Run this on the server as root within the next hour:",
			"")
		// The command keeps to one line where it fits, so it copies in one
		// piece; narrow terminals break it after the pipe, which the shell
		// reads as one command too.
		for _, line := range wrapCommand(m.added.InstallCommand, width-len(pad)-1) {
			added = append(added, pad+style.Accent.Render(line))
		}
		added = append(added, "", pad+style.Muted.Render("It turns online here when its agent connects."))
	}
	switch {
	case m.loading && m.offline == nil:
		return append([]string{pad + style.Muted.Render("Connecting to "+host(m.url)+"…")}, added...)
	case len(m.servers) == 0:
		return append(empty("No servers yet.", "Press a to add one, then run its install command on the server."), added...)
	}
	now := time.Now()
	rows := make([][]string, 0, len(m.servers))
	for index, server := range m.servers {
		rows = append(rows, []string{name(server.Name, index == m.selected), style.Server(server), server.System(), dash(server.AgentVersion), server.LastSeen(now), server.Access()})
	}
	tableRoom := -1
	if room >= 0 {
		tableRoom = max(room-len(added), 2)
	}
	return append(table([]string{"NAME", "STATUS", "SYSTEM", "AGENT", "LAST SEEN", "ACCESS"}, rows, m.selected, tableRoom), added...)
}

func (m model) appsView(room int) []string {
	if len(m.apps) == 0 {
		return empty("No apps yet.", fmt.Sprintf("Press d to deploy %s, or run %s deploy in a repo.", m.dir, build.Name))
	}
	rows := make([][]string, 0, len(m.apps))
	for index, app := range m.apps {
		rows = append(rows, []string{name(app.Name, index == m.appSelected), app.ServerName, style.App(app)})
	}
	return table([]string{"NAME", "SERVER", "STATUS"}, rows, m.appSelected, room)
}

func (m model) detailView(width, room int) []string {
	app := m.detail.App
	lines := []string{pad + style.Bold.Render(app.Name) + "   " + style.App(app)}
	facts := style.Muted.Render("on ") + app.ServerName
	for _, deploy := range m.detail.Deploys {
		if deploy.Status != "succeeded" || deploy.Detected == nil {
			continue
		}
		detected := deploy.Detected
		facts += dot() + detected.Kind + dot() + style.Muted.Render("service ") + detected.Service + dot() + style.Muted.Render("port ") + fmt.Sprint(detected.Port)
		lines = append(lines, pad+facts)
		for _, database := range detected.Databases {
			lines = append(lines, pad+style.Muted.Render("database ")+database.Path+dot()+style.Muted.Render(database.HostPath))
		}
		facts = ""
		break
	}
	if facts != "" {
		lines = append(lines, pad+facts)
	}
	lines = append(lines, "")

	if len(m.detail.Deploys) == 0 {
		lines = append(lines, pad+style.Muted.Render("No deploys yet. Press d to deploy "+m.dir+"."))
	} else {
		// The history gets up to 8 rows; with the output panel open it
		// leaves the panel at least 6 lines.
		history := min(len(m.detail.Deploys), 8)
		if m.outputTitle != "" && room >= 0 {
			history = max(min(history, room-len(lines)-1-4-6), 1)
		}
		rows := make([][]string, 0, history)
		for _, deploy := range m.detail.Deploys[:history] {
			rows = append(rows, m.deployRow(deploy, app.LiveNumber))
		}
		lines = append(lines, table([]string{"DEPLOY", "STATUS", "STARTED", "TOOK", "NOTE"}, rows, -1, -1)...)
	}

	if m.outputTitle != "" {
		lines = append(lines, "")
		lines = append(lines, m.panel(width, room-len(lines))...)
	}
	return lines
}

func (m model) deployRow(deploy client.Deploy, liveNumber int) []string {
	status := style.Deploy(deploy, liveNumber)
	took := "-"
	switch {
	case deploy.FinishedAt != nil:
		took = duration(deploy.FinishedAt.Sub(deploy.CreatedAt))
	case deploy.Status == "running":
		status = style.Warning.Render(style.Spinner[m.frame%len(style.Spinner)] + " running")
		took = duration(time.Since(deploy.CreatedAt))
	}
	note := deploy.Error
	if deploy.Commit != "" && note == "" {
		note = deploy.Commit[:min(7, len(deploy.Commit))] + " " + deploy.CommitMessage
	}
	if deploy.FromNumber > 0 {
		note = strings.TrimSpace(fmt.Sprintf("rollback to v%d %s", deploy.FromNumber, note))
	}
	switch {
	case note == "":
	case deploy.Status == "failed":
		note = style.Danger.Render(clip(note, 60))
	default:
		note = style.Muted.Render(clip(note, 60))
	}
	return []string{fmt.Sprintf("v%d", deploy.Number), status, style.Muted.Render(deploy.CreatedAt.Local().Format("Jan 2 15:04")), style.Muted.Render(took), note}
}

// panel is the output pane: a title with its state on the right, a hairline
// and the newest lines that fit, like a terminal.
func (m model) panel(width, room int) []string {
	right := m.outputNote
	if m.running {
		frame := style.Spinner[m.frame%len(style.Spinner)]
		elapsed := style.Muted.Render(" · " + duration(time.Since(m.runStarted)))
		right = style.Warning.Render(frame) + " " + clip(m.runStep, max(width/3, 12)) + elapsed
	}
	title := pad + style.Bold.Render(m.outputTitle)
	if gap := width - lipgloss.Width(title) - lipgloss.Width(right) - 2; right != "" && gap >= 2 {
		title += strings.Repeat(" ", gap) + right
	}
	lines := []string{title, pad + rule(width-len(pad)-2)}

	var output []string
	for _, line := range m.output {
		text, step := style.LogLine(line)
		switch {
		case !m.isDeployOutput():
			output = append(output, pad+outputLine(line))
		case text == "":
		case step:
			output = append(output, pad+style.Step(text))
		default:
			output = append(output, pad+"  "+style.Noise(text))
		}
	}
	if m.running && len(output) == 0 {
		output = append(output, pad+style.Muted.Render("waiting for the server…"))
	}
	if room >= 0 {
		keep := max(room-len(lines), 1)
		if len(output) > keep {
			output = output[len(output)-keep:]
		}
	}
	return append(lines, output...)
}

// isDeployOutput reports whether the panel shows a deploy log rather than
// container logs or env vars.
func (m model) isDeployOutput() bool {
	return !strings.HasPrefix(m.outputTitle, "logs of ") && !strings.HasPrefix(m.outputTitle, "env of ")
}

// outputLine mutes the container prefix of a log line, "web-1  | ", and the
// hidden values of env vars.
func outputLine(line string) string {
	if key, value, ok := strings.Cut(line, "="); ok && value == "••••••" {
		return key + style.Muted.Render("="+value)
	}
	prefix, rest, ok := strings.Cut(line, " | ")
	if !ok || strings.Contains(strings.TrimSpace(prefix), " ") {
		return line
	}
	return style.Muted.Render(prefix+" |") + " " + rest
}

func (m model) footer(width int) []string {
	lines := []string{rule(width)}
	prompt, keys := m.prompt()
	// A message wraps rather than lose its end; lines after the first
	// line up under its text.
	message := func(symbol string, color lipgloss.Style, text string) {
		for index, line := range wrap(text, width-4, "") {
			lead := color.Render(symbol)
			if index > 0 {
				lead = " "
			}
			lines = append(lines, " "+lead+" "+color.Render(line))
		}
	}
	// A result shows above an open prompt, such as the error of a query.
	switch {
	case m.err != nil:
		message(style.SymbolFailed, style.Danger, sentence(m.err.Error()))
	case m.notice != "":
		message(style.SymbolDone, style.Success, m.notice)
	}
	lines = append(lines, prompt...)
	return append(lines, hints(keys, width)...)
}

// prompt is the question or input waiting for an answer, and the keys that
// answer it. Without one, the keys are those of the place.
func (m model) prompt() ([]string, [][2]string) {
	// bar marks the prompt in its color; the detail goes on its own line
	// when both do not fit.
	bar := func(color lipgloss.Style, text string, detail ...string) []string {
		line := " " + color.Render(style.SymbolSelected) + " " + text
		if len(detail) == 0 {
			return []string{line}
		}
		if m.width == 0 || lipgloss.Width(line)+1+lipgloss.Width(detail[0]) <= m.width {
			return []string{line + " " + style.Muted.Render(detail[0])}
		}
		return []string{line, " " + color.Render(style.SymbolSelected) + " " + style.Muted.Render(detail[0])}
	}
	cursor := style.Accent.Render("█")
	cancel := [2]string{"any other key", "cancels"}
	switch m.mode {
	case modeAdd:
		return bar(style.Accent, style.Bold.Render("Name: ")+m.input+cursor), [][2]string{{"enter", "add"}, {"esc", "cancel"}}
	case modeEnv:
		return bar(style.Accent, style.Bold.Render("Env: ")+m.input+cursor), [][2]string{{"KEY=value", "set"}, {"-KEY", "remove"}, {"enter", "apply"}, {"esc", "close"}}
	case modeImage:
		return bar(style.Accent, style.Bold.Render("Image: ")+m.input+cursor), [][2]string{{"enter", "deploy"}, {"esc", "cancel"}}
	case modeSQL:
		return bar(style.Accent, style.Bold.Render("SQL: ")+m.input+cursor), [][2]string{{"enter", "run"}, {"esc", "close"}}
	case modeBackups:
		return bar(style.Accent, style.Bold.Render("Backups: ")+m.input+cursor), [][2]string{{"enter", "back up now"}, {"snapshot or 14:02", "restore"}, {"esc", "close"}}
	case modeConfirmRestore:
		return bar(style.Warning, style.Bold.Render("Restore "+m.pendingRestore+"?"), "ops takes a snapshot of the current state first."), [][2]string{{"y", "restore"}, cancel}
	case modeConfirmWrite:
		return bar(style.Warning, style.Bold.Render("This query changes data. Run it?"), m.pendingSQL), [][2]string{{"y", "run"}, cancel}
	case modeDomains:
		return bar(style.Accent, style.Bold.Render("Domain: ")+m.input+cursor), [][2]string{{"blog.de", "add"}, {"-blog.de", "remove"}, {"=blog.de", "canonical"}, {"esc", "close"}}
	case modeProtect:
		masked := m.input
		if user, password, found := strings.Cut(m.input, ":"); found {
			masked = user + ":" + strings.Repeat("•", len([]rune(password)))
		}
		return bar(style.Accent, style.Bold.Render("Protect: ")+masked+cursor), [][2]string{{"user:password", "protect"}, {"off", "public"}, {"esc", "cancel"}}
	case modeConfirmRemove:
		question, detail := "", ""
		if m.tab == tabServers && m.selected < len(m.servers) {
			question, detail = "Remove "+m.servers[m.selected].Name+"?", "Its agent is disconnected."
		} else if app, ok := m.currentApp(); ok {
			question, detail = "Remove "+app.Name+"?", "Its containers stop, its data stays on the server."
		}
		return bar(style.Danger, style.Bold.Render(question), detail), [][2]string{{"y", "remove"}, cancel}
	case modeConfirmSecurity:
		return bar(style.Warning, style.Bold.Render(m.pendingSecurity.question), m.pendingSecurity.detail), [][2]string{{"y", "apply"}, cancel}
	case modeConfirmRollback:
		app, _ := m.currentApp()
		question := fmt.Sprintf("Roll %s back to the deploy before v%d?", app.Name, app.LiveNumber)
		return bar(style.Warning, style.Bold.Render(question), "It starts the old images with the env they had."), [][2]string{{"y", "roll back"}, cancel}
	}

	here := client.AppName("", m.dir)
	appKeys := [][2]string{{"d", "deploy " + here}, {"g", "deploy from GitHub"}, {"i", "image"}, {"R", "redeploy"}, {"l", "logs"}, {"e", "env"}, {"s", "sql"}, {"b", "backups"}, {"o", "domains"}, {"p", "protect"}, {"h", "hook"}, {"r", "rollback"}, {"x", "remove"}}
	switch {
	case m.tab == tabServers && m.added != nil:
		return nil, [][2]string{{"esc", "close"}, {"tab", "apps"}, {"↑↓", "select"}, {"a", "add"}, {"x", "remove"}, {"q", "quit"}}
	case m.tab == tabActivity:
		return nil, [][2]string{{"tab", "servers"}, {"q", "quit"}}
	case m.tab == tabServers:
		return nil, [][2]string{{"tab", "apps"}, {"↑↓", "select"}, {"a", "add"}, {"t", "tunnel"}, {"f", "firewall"}, {"x", "remove"}, {"q", "quit"}}
	case m.detail != nil:
		return nil, append(appKeys, [2]string{"esc", "back"}, [2]string{"q", "quit"})
	}
	keys := [][2]string{{"tab", "activity"}, {"↑↓", "select"}, {"enter", "open"}}
	return nil, append(append(keys, appKeys...), [2]string{"q", "quit"})
}

// hints lays out keys over as many lines as the width needs.
func hints(keys [][2]string, width int) []string {
	var lines []string
	line := pad
	for _, key := range keys {
		item := style.Bold.Render(key[0]) + " " + style.Muted.Render(key[1])
		if line != pad && lipgloss.Width(line)+3+lipgloss.Width(item) > width {
			lines, line = append(lines, line), pad
		}
		if line != pad {
			line += "   "
		}
		line += item
	}
	return append(lines, line)
}

// table aligns rows under a muted header. The selected row gets the accent
// bar in the gutter; -1 selects none. With room >= 0 it shows at most that
// many lines and scrolls to keep the selection in view.
func table(header []string, rows [][]string, selected, room int) []string {
	widths := make([]int, len(header))
	for _, row := range append([][]string{header}, rows...) {
		for column, cell := range row {
			widths[column] = max(widths[column], lipgloss.Width(cell))
		}
	}
	line := func(row []string) string {
		cells := make([]string, len(row))
		for column, cell := range row {
			cells[column] = cell + strings.Repeat(" ", widths[column]-lipgloss.Width(cell))
		}
		return strings.TrimRight(strings.Join(cells, "   "), " ")
	}
	start, end := 0, len(rows)
	if room >= 0 && len(rows) > room-1 {
		visible := max(room-1, 1)
		start = min(max(selected-visible+1, 0), len(rows)-visible)
		end = start + visible
	}
	lines := []string{pad + style.Muted.Render(line(header))}
	for index, row := range rows[start:end] {
		gutter := pad
		if start+index == selected {
			gutter = " " + style.Accent.Render(style.SymbolSelected) + " "
		}
		lines = append(lines, gutter+line(row))
	}
	return lines
}

// name is a name in a list: in the accent when selected.
func name(text string, selected bool) string {
	if selected {
		return style.Accent.Bold(true).Render(text)
	}
	return style.Bold.Render(text)
}

// empty is the generous empty state of a list.
func empty(title, next string) []string {
	return []string{"", pad + style.Bold.Render(title), "", pad + style.Muted.Render(next)}
}

func dot() string { return style.Dim.Render("  ·  ") }

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func duration(d time.Duration) string { return style.Duration(d) }

// sentence capitalizes the first word, but not one like "v4".
func sentence(text string) string {
	runes := []rune(text)
	if len(runes) < 2 || !unicode.IsLower(runes[1]) {
		return text
	}
	return string(unicode.ToUpper(runes[0])) + string(runes[1:])
}

// clip cuts plain text to width cells and ends it with … when it was longer.
func clip(text string, width int) string {
	if width <= 1 || lipgloss.Width(text) <= width {
		return text
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(text) + "…"
}

// fit cuts a styled line to the width of the terminal.
func fit(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}

// wrapCommand breaks a shell command that is wider than width after its
// pipes, where the shell goes on reading the next line.
func wrapCommand(command string, width int) []string {
	if lipgloss.Width(command) <= width {
		return []string{command}
	}
	parts := strings.Split(command, " | ")
	for index := range parts[:len(parts)-1] {
		parts[index] += " |"
	}
	return parts
}

// wrap breaks text at spaces into lines of at most width cells; the lines
// after the first start with indent.
func wrap(text string, width int, indent string) []string {
	if width <= 10 || lipgloss.Width(text) <= width {
		return []string{text}
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && line != indent && lipgloss.Width(line)+1+lipgloss.Width(word) > width {
			lines, line = append(lines, line), indent
		}
		if line != "" && line != indent {
			line += " "
		}
		line += word
	}
	return append(lines, line)
}
