package tui

import (
	"fmt"
	"net/url"
	"slices"
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
	label := func(name string, active bool) string {
		if active {
			return style.Accent.Bold(true).Render(name)
		}
		return style.Muted.Render(name)
	}
	activity := label("activity", m.tab == tabActivity)
	team := label("team", m.tab == tabTeam)
	settings := label("settings", m.tab == tabSettings)
	if m.tab == tabApps && m.detail != nil {
		apps = style.Accent.Bold(true).Render("apps") + style.Muted.Render(" › ") + style.Bold.Render(m.detail.App.Name)
	}
	left := " " + style.Mark.Render(build.Name) + "   " + servers + "   " + apps + "   " + activity + "   " + team + "   " + settings

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
	case m.mode == modeHelp:
		return m.helpView(room)
	case m.tab == tabServers:
		return m.serversView(width, room)
	case m.tab == tabActivity:
		return m.activityView(room)
	case m.tab == tabTeam:
		return m.teamView(room)
	case m.tab == tabSettings:
		return m.settingsView(room)
	case m.detail != nil:
		return m.detailView(width, room)
	}
	return m.appsView(width, room)
}

func (m model) serversView(width, room int) []string {
	var added []string
	if m.added != nil {
		lead := " added. Run this on the server as root (or put sudo before sh) within the next hour:"
		if m.again {
			lead = ": a new install command. Run it on the server as root (or put sudo before sh) within the next hour; the old one stops working:"
		}
		added = append(added, "",
			pad+style.Success.Render(style.SymbolDone)+" "+style.Bold.Render(m.added.Server.Name)+lead,
			"")
		// The command keeps to one line where it fits, so it copies in one
		// piece; narrow terminals break it after the pipe, which the shell
		// reads as one command too.
		for _, line := range wrapCommand(m.added.InstallCommand, width-len(pad)-1) {
			added = append(added, pad+style.Accent.Render(line))
		}
		added = append(added, "", pad+style.Muted.Render("It shows once: ops stores only a fingerprint of it. The server turns online here when its agent connects."))
	}
	if m.removed != nil {
		added = append(added, "",
			pad+style.Success.Render(style.SymbolDone)+" "+style.Bold.Render(m.removed.name)+" removed. Its agent is still installed; remove it on the server as root (or put sudo in front) with:",
			"")
		for _, line := range wrapCommand(m.removed.uninstall, width-len(pad)-1) {
			added = append(added, pad+style.Accent.Render(line))
		}
		kept := "The apps keep running with their data. Add --apps to remove them too."
		if len(m.removed.apps) > 0 {
			kept = "It was offline, so its apps went with it: " + strings.Join(m.removed.apps, ", ") + ". They stay on the server with their data if it comes back; add --apps to remove them too."
		}
		added = append(added, "", pad+style.Muted.Render(kept))
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
		rows = append(rows, []string{name(server.Name, index == m.selected), style.Server(server), server.System(), dash(server.AgentVersion), server.LastSeen(now), server.Access(), server.RulesText()})
	}
	tableRoom := -1
	if room >= 0 {
		tableRoom = max(room-len(added), 2)
	}
	return append(table([]string{"NAME", "STATUS", "SYSTEM", "AGENT", "LAST SEEN", "ACCESS", "RULES"}, rows, m.selected, tableRoom), added...)
}

func (m model) appsView(width, room int) []string {
	if len(m.apps) == 0 {
		return empty("No apps yet.", fmt.Sprintf("Press a to create one, or run %s deploy in a repo to create and deploy it.", build.Name))
	}
	rows := make([][]string, 0, len(m.apps))
	for index, app := range m.apps {
		rows = append(rows, []string{name(app.Name, index == m.appSelected), app.ServerName, style.App(app)})
	}
	if m.outputTitle == "" {
		return table([]string{"NAME", "SERVER", "STATUS"}, rows, m.appSelected, room)
	}
	// With the output panel open the list leaves it at least 6 lines, like
	// the history of an open app.
	tableRoom := -1
	if room >= 0 {
		tableRoom = max(min(len(rows)+1, room-1-6), 2)
	}
	lines := append(table([]string{"NAME", "SERVER", "STATUS"}, rows, m.appSelected, tableRoom), "")
	return append(lines, m.panel(width, room-len(lines))...)
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
		next := "No deploys yet. Press i to deploy an image or gh to deploy from GitHub, or run " + build.Name + " deploy in its repo."
		if app.Name == client.AppName("", m.dir) {
			next = "No deploys yet. Press d to deploy " + m.dir + "."
		}
		lines = append(lines, pad+style.Muted.Render(next))
	} else {
		// The history gets up to historyRows rows; with the output panel
		// open it leaves the panel at least 6 lines, and keeps the pick in
		// view.
		history := min(len(m.detail.Deploys), historyRows)
		if m.outputTitle != "" && room >= 0 {
			history = max(min(history, room-len(lines)-1-4-6), 1)
		}
		rows := make([][]string, 0, historyRows)
		selected := -1
		for index, deploy := range m.detail.Deploys[:min(len(m.detail.Deploys), historyRows)] {
			if deploy.Number == m.deployPick {
				selected = index
			}
			rows = append(rows, m.deployRow(deploy, app.LiveNumber))
		}
		lines = append(lines, table([]string{"DEPLOY", "STATUS", "STARTED", "TOOK", "NOTE"}, rows, selected, history+1)...)
	}

	if m.outputTitle != "" {
		lines = append(lines, "")
		lines = append(lines, m.panel(width, room-len(lines))...)
	}
	return lines
}

// historyRows is how many deploys the open app lists and lets you pick.
const historyRows = 8

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
// and the newest lines that fit, like a terminal. Scrolled up, it shows how
// many lines are below; the line the search found has the accent bar.
func (m model) panel(width, room int) []string {
	output, _ := m.panelLines()
	if m.running && len(output) == 0 {
		output = append(output, pad+style.Muted.Render("waiting for the server…"))
	}
	scroll := 0
	if room >= 0 {
		keep := max(room-2, 1)
		if m.shared != nil {
			m.shared.panelKeep = keep
		}
		scroll = min(m.outputScroll, max(len(output)-keep, 0))
		end := len(output) - scroll
		start := max(end-keep, 0)
		if match := m.panelMatch - 1; m.search != "" && match >= start && match < end {
			output = slices.Clone(output)
			output[match] = " " + style.Accent.Render(style.SymbolSelected) + " " + strings.TrimPrefix(output[match], pad)
		}
		output = output[start:end]
	}

	right := m.outputNote
	if m.running {
		frame := style.Spinner[m.frame%len(style.Spinner)]
		elapsed := style.Muted.Render(" · " + duration(time.Since(m.runStarted)))
		right = style.Warning.Render(frame) + " " + clip(m.runStep, max(width/3, 12)) + elapsed
	}
	if scroll > 0 {
		more := style.Muted.Render(fmt.Sprintf("%d more below · G follows", scroll))
		if right == "" {
			right = more
		} else {
			right = more + style.Dim.Render("  ·  ") + right
		}
	}
	titleStyle := style.Bold
	if m.panelFocused() {
		titleStyle = style.Accent.Bold(true)
	}
	title := pad + titleStyle.Render(m.outputTitle)
	if gap := width - lipgloss.Width(title) - lipgloss.Width(right) - 2; right != "" && gap >= 2 {
		title += strings.Repeat(" ", gap) + right
	}
	return append([]string{title, pad + rule(width-len(pad)-2)}, output...)
}

// panelLines are the lines the output panel draws, each with the index of
// the output line it comes from.
func (m model) panelLines() ([]string, []int) {
	var lines []string
	var raw []int
	for index, line := range m.output {
		text, step := style.LogLine(line)
		switch {
		case !m.isDeployOutput():
			lines = append(lines, pad+outputLine(line))
		case text == "":
			continue
		case step:
			lines = append(lines, pad+style.Step(text))
		default:
			lines = append(lines, pad+"  "+style.Noise(text))
		}
		raw = append(raw, index)
	}
	return lines, raw
}

// isDeployOutput reports whether the panel shows a deploy log rather than
// container logs or env vars.
func (m model) isDeployOutput() bool {
	for _, prefix := range []string{"logs of ", "env of ", "GitHub of "} {
		if strings.HasPrefix(m.outputTitle, prefix) {
			return false
		}
	}
	return true
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
	// Like vim's showcmd: the count and the first key of a pair, waiting.
	if waiting := m.pending; m.count > 0 || waiting != "" {
		if m.count > 0 {
			waiting = fmt.Sprint(m.count) + waiting
		}
		keys = append([][2]string{{waiting, "…"}}, keys...)
	}
	if m.mode == modeList && len(keys) > 0 && keys[len(keys)-1] == [2]string{"q", "quit"} {
		keys = append(keys[:len(keys)-1:len(keys)-1], [2]string{"?", "help"}, [2]string{"q", "quit"})
	}
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
	case modeSearch:
		return bar(style.Accent, style.Bold.Render("/")+m.input+cursor), [][2]string{{"enter", "search"}, {"esc", "cancel"}}
	case modeCommand:
		return bar(style.Accent, style.Bold.Render(":")+m.input+cursor), [][2]string{{"enter", "run"}, {"tab", "complete"}, {"esc", "cancel"}}
	case modeHelp:
		return nil, [][2]string{{"j k", "scroll"}, {"ctrl+d ctrl+u", "half a page"}, {"gg G", "top, end"}, {"esc", "close"}}
	case modeAdd:
		return bar(style.Accent, style.Bold.Render("Name: ")+m.input+cursor), [][2]string{{"enter", "add, empty for a name like brave-otter"}, {"esc", "cancel"}}
	case modeEnv:
		toggle := [2]string{"show", "values"}
		if m.envShown {
			toggle = [2]string{"hide", "values"}
		}
		return bar(style.Accent, style.Bold.Render("Env: ")+m.input+cursor), [][2]string{{"KEY=value", "set"}, {"-KEY", "remove"}, toggle, {"edit", "all in $EDITOR"}, {"enter", "apply"}, {"esc", "close"}}
	case modeImage:
		return bar(style.Accent, style.Bold.Render("Image: ")+m.input+cursor), [][2]string{{"enter", "deploy"}, {"esc", "cancel"}}
	case modeSQL:
		return bar(style.Accent, style.Bold.Render("SQL: ")+m.input+cursor), [][2]string{{"enter", "run"}, {"esc", "close"}}
	case modeBackups:
		return bar(style.Accent, style.Bold.Render("Backups: ")+m.input+cursor), [][2]string{{"enter", "back up now"}, {"snapshot or 14:02", "restore"}, {"on/off", "snapshot before changes"}, {"esc", "close"}}
	case modeGitHub:
		return bar(style.Accent, style.Bold.Render("GitHub: ")+m.input+cursor), [][2]string{{"enter", "deploy the branch"}, {"branch, tag or commit", "deploy it"}, {"link owner/repo", "follow"}, {"unlink", "stop"}, {"esc", "close"}}
	case modeForm:
		field := m.form.fields[m.form.step]
		shown := m.input
		if field.secret {
			shown = strings.Repeat("•", len([]rune(m.input)))
		}
		label := fmt.Sprintf("%s · %s (%d/%d): ", m.form.title, field.label, m.form.step+1, len(m.form.fields))
		next := "next"
		if m.form.step == len(m.form.fields)-1 {
			next = "save"
		}
		keys := [][2]string{{"enter", next}, {"esc", "cancel"}}
		if field.hint == "" {
			return bar(style.Accent, style.Bold.Render(label)+shown+cursor), keys
		}
		return bar(style.Accent, style.Bold.Render(label)+shown+cursor, field.hint), keys
	case modeConfirm:
		color := style.Warning
		if m.confirm.danger {
			color = style.Danger
		}
		return bar(color, style.Bold.Render(m.confirm.question), m.confirm.detail), [][2]string{{"y", m.confirm.verb}, cancel}
	case modeConfirmName:
		return bar(style.Danger, style.Bold.Render(m.confirm.question)+" "+m.confirm.detail, "Type "+m.confirm.name+" to confirm: "+m.input+cursor), [][2]string{{"enter", "confirm"}, {"esc", "cancel"}}
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
	case modeConfirmSecurity:
		return bar(style.Warning, style.Bold.Render(m.pendingSecurity.question), m.pendingSecurity.detail), [][2]string{{"y", "apply"}, cancel}
	case modeConfirmStop:
		app, _ := m.currentApp()
		return bar(style.Warning, style.Bold.Render("Stop "+app.Name+"?"), "It is offline until you start it; its data stays."), [][2]string{{"y", "stop"}, cancel}
	case modeConfirmRollback:
		app, _ := m.currentApp()
		question := fmt.Sprintf("Roll %s back to the deploy before v%d?", app.Name, app.LiveNumber)
		if m.pendingRollback != 0 {
			question = fmt.Sprintf("Roll %s back to v%d?", app.Name, m.pendingRollback)
		}
		return bar(style.Warning, style.Bold.Render(question), "It starts the old images with the env they had."), [][2]string{{"y", "roll back"}, cancel}
	}

	// Only keys that work here show: d needs the app of the directory,
	// the app keys an app.
	var appKeys [][2]string
	if here := client.AppName("", m.dir); m.hasApp(here) {
		appKeys = append(appKeys, [2]string{"d", "deploy " + here})
	}
	rollback := [2]string{"r", "rollback"}
	if m.detail != nil && m.deployPick != 0 {
		rollback = [2]string{"r", fmt.Sprintf("roll back to v%d", m.deployPick)}
	}
	power := [2]string{"S", "stop"}
	if app, ok := m.currentApp(); ok && app.Stopped {
		power = [2]string{"S", "start"}
	}
	appKeys = append(appKeys, [][2]string{{"gh", "GitHub"}, {"i", "image"}, {"R", "redeploy"}, {"t", "restart"}, power, {"L", "logs"}, {"e", "env"}, {"s", "sql"}, {"b", "backups"}, {"o", "domains"}, {"p", "protect"}, rollback, {"x", "remove"}}...)
	switch {
	case m.panelFocused():
		return nil, [][2]string{{"j k", "scroll"}, {"ctrl+d ctrl+u", "half a page"}, {"gg G", "top, follow"}, {"/", "search"}, {"n N", "next, previous"}, {"ctrl+w", "the list"}, {"esc", "back"}, {"q", "quit"}}
	case m.tab == tabServers && (m.added != nil || m.removed != nil):
		return nil, [][2]string{{"esc", "close"}, {"tab", "apps"}, {"j k", "select"}, {"a", "add"}, {"x", "remove"}, {"q", "quit"}}
	case m.tab == tabActivity:
		return nil, m.activityKeys()
	case m.tab == tabTeam:
		return nil, m.teamKeys()
	case m.tab == tabSettings:
		keys := [][2]string{{"tab", "servers"}, {"j k", "select"}, {"enter", "set"}, {"a", "add a registry"}}
		if rows := m.settingRows(); m.settingSelected < len(rows) && rows[m.settingSelected].set {
			keys = append(keys, [2]string{"x", "remove"})
		}
		return nil, append(keys, [2]string{"q", "quit"})
	case m.tab == tabServers && len(m.servers) == 0:
		return nil, [][2]string{{"tab", "apps"}, {"a", "add"}, {"q", "quit"}}
	case m.tab == tabServers:
		return nil, [][2]string{{"tab", "apps"}, {"j k", "select"}, {"a", "add"}, {"t", "tunnel"}, {"f", "firewall"}, {"s", "ssh on tailnet"}, {"cw", "rename"}, {"i", "install command"}, {"u", "update agent"}, {"K J", "move"}, {"x", "remove"}, {"q", "quit"}}
	case m.detail != nil:
		var keys [][2]string
		if len(m.detail.Deploys) > 0 {
			keys = append(keys, [2]string{"j k", "pick a deploy"})
		}
		if m.deployPick != 0 {
			keys = append(keys, [2]string{"l", "its log"})
		}
		return nil, append(append(keys, appKeys...), [2]string{"esc", "back"}, [2]string{"q", "quit"})
	case len(m.apps) == 0:
		return nil, [][2]string{{"tab", "activity"}, {"a", "create an app"}, {"q", "quit"}}
	}
	keys := [][2]string{{"tab", "activity"}, {"j k", "select"}, {"enter", "open"}, {"a", "create"}, {"K J", "move"}}
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

// plural counts a word: "1 row", "2 rows".
func plural(count int, word string) string {
	if count == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", count, word)
}
