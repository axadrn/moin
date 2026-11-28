package tui

import (
	"fmt"
	"slices"
	"strconv"
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

// The screen, top to bottom:
//
//	 moin   Apps 2 · Servers 1 · Backups 2           ✓ all good
//	╭─ Apps ──────────────╮ ╭─ shop ───────────────────────────╮
//	│ server-1            │ │ ● running   v3, deployed 4m ago   │
//	│ ▌ shop   ● running  │ │                                   │
//	│   blog   ○ stopped  │ │ Address   https://shop.example.com│
//	╰─────────────────────╯ ╰───────────────────────────────────╯
//	 ◐ restarting shop · health check · 3s
//	 ↑↓ select  enter logs  r restart  b rollback  ? help  q quit

// View draws the screen.
func (m *model) View() tea.View {
	rows := max(m.height-3, 5)
	body := m.body(rows)
	screen := strings.Join(append([]string{m.header()}, body...), "\n")
	if overlay := m.overlay(); overlay != "" {
		// The overlay floats in the middle of the body.
		x := max((m.width-lipgloss.Width(overlay))/2, 0)
		y := 1 + max((rows-lipgloss.Height(overlay))/2, 0)
		screen = lipgloss.NewCompositor(
			lipgloss.NewLayer(screen),
			lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
		).Render()
	}
	lines := strings.Split(screen, "\n")
	for len(lines) < rows+1 {
		lines = append(lines, "")
	}
	lines = append(lines[:rows+1], m.statusLine(), m.keys())
	for index, line := range lines {
		lines[index] = fit(line, m.width)
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	return view
}

// header is one line: the mark of moin, the tabs with their counts, and
// at the right how everything is, flush with the panes below.
func (m *model) header() string {
	left := style.Mark.Render(build.Name) + "   " + m.tabs()
	right := m.summary()
	return left + strings.Repeat(" ", max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)) + right
}

// summary says in one chip how everything is: the worst thing first.
func (m *model) summary() string {
	asking, down, needs := 0, 0, 0
	for _, server := range m.servers {
		switch {
		case !server.answered:
			asking++
		case server.err != nil:
			down++
		default:
			needs += len(m.needs(server))
		}
	}
	switch {
	case down > 0:
		return style.Danger.Render(style.SymbolFailed + " " + count(down, "server") + " unreachable")
	case needs == 1:
		return style.Warning.Render("! 1 thing needs you")
	case needs > 1:
		return style.Warning.Render("! " + strconv.Itoa(needs) + " things need you")
	case asking > 0:
		return style.Warning.Render(m.spinner() + " asking " + count(asking, "server"))
	}
	return style.Success.Render(style.SymbolDone + " all good")
}

// tabs are the names of the three lists with their counts, the open
// one bold and underlined.
func (m *model) tabs() string {
	counts := []int{len(m.apps()), len(m.servers), len(m.apps())}
	parts := []string{}
	for index, name := range tabNames {
		number := style.Muted.Render(" " + strconv.Itoa(counts[index]))
		if tab(index) == m.tab && m.app == nil {
			parts = append(parts, lipgloss.NewStyle().Bold(true).Underline(true).Render(name)+number)
			continue
		}
		parts = append(parts, style.Muted.Render(name)+number)
	}
	return strings.Join(parts, style.Dim.Render("  ·  "))
}

// body is the list and the side, or the logs of the open app.
func (m *model) body(rows int) []string {
	if m.app != nil {
		return m.logsBox(rows)
	}
	list, side, title := m.listLines(), m.sideLines(), m.sideTitle()
	if m.width < 72 {
		return box(tabNames[m.tab], scrolled(list, rows-2), m.width, rows)
	}
	listWidth := min(max(m.width*2/5, 30), 48)
	left := box(tabNames[m.tab], scrolled(list, rows-2), listWidth, rows)
	right := box(title, side, m.width-listWidth-1, rows)
	lines := make([]string, rows)
	for index := range lines {
		lines[index] = left[index] + " " + right[index]
	}
	return lines
}

// listRow is one row of a list: what it shows and whether it is selected.
type listRow struct {
	text     string
	selected bool
}

// listLines are the rows of the open tab's list.
func (m *model) listLines() []listRow {
	if m.tab == serversTab {
		return m.serverRows()
	}
	nameWidth := 0
	for _, key := range m.apps() {
		nameWidth = max(nameWidth, lipgloss.Width(key.app))
	}
	var rows []listRow
	for _, server := range m.servers {
		head := style.Muted.Render(server.name)
		switch {
		case !server.answered:
			head += "  " + style.Warning.Render(m.spinner())
		case server.err != nil:
			head += "  " + style.Danger.Render(style.SymbolFailed+" unreachable")
		}
		if len(rows) > 0 {
			rows = append(rows, listRow{})
		}
		rows = append(rows, listRow{text: head})
		if server.answered && server.err == nil && len(server.status.Apps) == 0 {
			rows = append(rows, listRow{text: "  " + style.Dim.Render("no apps yet")})
		}
		for _, app := range server.status.Apps {
			if server.err != nil {
				break
			}
			key := appKey{server.name, app.Name}
			name := pad(app.Name, nameWidth)
			state := appState(app.Running) + "  " + style.Muted.Render(releaseName(app.Release))
			switch {
			case m.action != nil && m.action.key == key:
				state = style.Warning.Render(m.spinner() + " " + m.action.verb)
			case m.tab == backupsTab:
				state = m.backupChip(key)
			}
			rows = append(rows, m.row(key == m.selected, name+"   "+state))
		}
	}
	return rows
}

// serverRows are the rows of the servers tab.
func (m *model) serverRows() []listRow {
	nameWidth := 0
	for _, server := range m.servers {
		nameWidth = max(nameWidth, lipgloss.Width(server.name))
	}
	var rows []listRow
	for _, server := range m.servers {
		state := style.Badge(style.StatusLive, "online") + "  " + style.Muted.Render(count(len(server.status.Apps), "app"))
		switch {
		case !server.answered:
			state = style.Warning.Render(m.spinner() + " asking")
		case server.err != nil:
			state = style.Badge(style.StatusFailed, "unreachable")
		case len(m.needs(server)) > 0:
			state = style.Warning.Render("! needs you") + "  " + style.Muted.Render(count(len(server.status.Apps), "app"))
		}
		rows = append(rows, m.row(server.name == m.selectedServer, pad(server.name, nameWidth)+"   "+state))
	}
	return rows
}

// row is a row of a list, marked and bold when it is selected.
func (m *model) row(selected bool, text string) listRow {
	if selected {
		return listRow{text: style.Bold.Render(style.SymbolSelected + " " + text), selected: true}
	}
	return listRow{text: "  " + text}
}

// scrolled are the rows that fit, scrolled so the selected one shows.
func scrolled(rows []listRow, room int) []string {
	first := 0
	for index, row := range rows {
		if row.selected && index >= room {
			first = index - room + 1
		}
	}
	lines := []string{}
	for _, row := range rows[first:] {
		lines = append(lines, row.text)
	}
	return lines
}

// sideTitle is the name of what the side shows.
func (m *model) sideTitle() string {
	if m.tab == serversTab {
		return m.selectedServer
	}
	if _, ok := m.appStatus(m.selected); ok {
		return m.selected.app
	}
	return "moin"
}

// sideLines are the facts of the selected app or server.
func (m *model) sideLines() []string {
	if m.tab == serversTab {
		return m.serverFacts()
	}
	app, ok := m.appStatus(m.selected)
	if !ok {
		return m.emptyLines()
	}
	if m.tab == backupsTab {
		return m.backupFacts(m.selected)
	}
	known := m.facts[m.selected]
	lines := []string{appState(app.Running) + "   " + releaseName(app.Release) + m.deployedAgo(known)}
	if !app.Running {
		lines = append(lines, "", style.Warning.Render("! "+app.Name+" does not run.")+style.Muted.Render(" Its logs say why: enter"))
	}
	lines = append(lines, "", fact("Server", m.selected.server))
	addresses := app.Addresses
	if len(addresses) == 0 {
		addresses = []string{style.Muted.Render("none yet: ") + style.Accent.Render(build.Name+" domains add "+app.Name+" <domain>")}
	}
	for index, address := range addresses {
		label := "Domain"
		if index > 0 {
			label = ""
		}
		lines = append(lines, fact(label, address))
	}
	lines = append(lines, fact("Deploys", m.releasesLine(known)))
	switch {
	case known == nil || !known.loaded:
		lines = append(lines, fact("Backups", style.Warning.Render(m.spinner())+style.Muted.Render(" asking")))
	case known.err != "":
		lines = append(lines, fact("Backups", failed(known.err)))
	case len(known.backups) == 0:
		lines = append(lines, fact("Backups", style.Muted.Render("no database to copy")))
	}
	if known != nil {
		for index, database := range known.backups {
			label := "Backups"
			if index > 0 {
				label = ""
			}
			lines = append(lines, fact(label, database.Database+"  "+backupState(database, time.Now())))
		}
	}
	server, _ := m.serverNamed(m.selected.server)
	return append(lines, needLines(serverNeeds(server))...)
}

// deployedAgo says when the live release was deployed, from the facts.
func (m *model) deployedAgo(known *facts) string {
	if known == nil {
		return ""
	}
	for _, release := range known.releases {
		if release.Live {
			return style.Muted.Render(", deployed " + human.Ago(release.At, time.Now()))
		}
	}
	return ""
}

// releasesLine says how many releases a rollback can go back to.
func (m *model) releasesLine(known *facts) string {
	switch {
	case known == nil || !known.loaded:
		return style.Warning.Render(m.spinner()) + style.Muted.Render(" asking")
	case known.err != "":
		return failed(known.err)
	}
	older := 0
	for _, release := range known.releases {
		if !release.Live {
			older++
		}
	}
	if older == 0 {
		return style.Muted.Render("only this one")
	}
	return fmt.Sprintf("%d older", older) + style.Muted.Render(", b goes back to one")
}

// emptyLines are the side before there is any app: how to deploy one.
func (m *model) emptyLines() []string {
	for _, server := range m.servers {
		if !server.answered {
			return []string{style.Warning.Render(m.spinner()) + " Asking your servers what runs there."}
		}
	}
	server := "<server>"
	if len(m.servers) > 0 {
		server = m.servers[0].name
	}
	return []string{
		style.Bold.Render("No apps yet."),
		"",
		style.Muted.Render("Deploy one from its folder:"),
		"",
		"  " + style.Accent.Render(build.Name+" deploy "+server),
	}
}

// serverFacts are the facts of the selected server.
func (m *model) serverFacts() []string {
	server, ok := m.serverNamed(m.selectedServer)
	switch {
	case !ok:
		return nil
	case !server.answered:
		return []string{style.Warning.Render(m.spinner()) + " Asking " + server.name + "."}
	case server.err != nil:
		return []string{
			style.Badge(style.StatusFailed, "unreachable"),
			"",
			m.cli.Explain(server.err),
			"",
			style.Muted.Render("Check that this works: ") + style.Accent.Render("ssh "+server.name),
		}
	}
	status := server.status
	running := 0
	for _, app := range status.Apps {
		if app.Running {
			running++
		}
	}
	apps := count(len(status.Apps), "app")
	if len(status.Apps) > 0 {
		apps += style.Muted.Render(fmt.Sprintf(", %d running", running))
	}
	lines := []string{style.Badge(style.StatusLive, "online") + "   " + style.Muted.Render(build.Name+" "+status.Version), "", fact("Apps", apps)}
	if len(status.Watchers) == 0 {
		lines = append(lines, fact("Watchers", style.Warning.Render("none. Nobody notices when it is gone")),
			fact("", style.Accent.Render(build.Name+" watcher connect "+server.name+" <url> <token>")))
	}
	for index, watcher := range status.Watchers {
		label := "Watchers"
		if index > 0 {
			label = ""
		}
		lines = append(lines, fact(label, watcher))
	}
	if status.PublicSSH {
		lines = append(lines, fact("SSH", style.Danger.Render("port 22 is open to the internet")),
			fact("", style.Accent.Render(build.Name+" secure "+server.name)))
	} else {
		lines = append(lines, fact("SSH", style.Success.Render(style.SymbolDone)+" only over Tailscale"))
	}
	knocks := style.Muted.Render("none today")
	if status.KnocksToday > 0 {
		knocks = human.Knocks(status.KnocksToday)
	}
	lines = append(lines, fact("Knocks", knocks))
	return append(lines, needLines(status.Needs)...)
}

// backupFacts are the backups of an app: each database with its check
// and its copies.
func (m *model) backupFacts(key appKey) []string {
	known := m.facts[key]
	switch {
	case known == nil || !known.loaded:
		return []string{style.Warning.Render(m.spinner()) + " Asking " + key.server + " for the copies of " + key.app + "."}
	case known.err != "":
		return []string{failed(known.err)}
	case len(known.backups) == 0:
		return []string{style.Muted.Render(key.app + " has no database " + build.Name + " can copy: no SQLite file on a volume, no Postgres service.")}
	}
	now := time.Now()
	var lines []string
	for _, database := range known.backups {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, style.Bold.Render(database.Database)+"  "+style.Muted.Render(database.Kind))
		lines = append(lines, checkState(database, now))
		if database.Error != "" {
			lines = append(lines, failed(database.Error))
		}
		if len(database.Copies) == 0 {
			lines = append(lines, style.Warning.Render("! no copy yet")+style.Muted.Render(". c takes one now"))
			continue
		}
		lines = append(lines, "")
		for index, copy := range database.Copies {
			if index == 5 {
				lines = append(lines, style.Muted.Render(fmt.Sprintf("  and %d older", len(database.Copies)-5)))
				break
			}
			note := style.Muted.Render(human.Size(float64(copy.Size)))
			if copy.ID == database.CheckedCopy && database.CheckError == "" {
				note += "  " + style.Success.Render(style.SymbolDone+" checked")
			}
			lines = append(lines, "  "+pad(copy.ID, 30)+pad(human.Ago(copy.At, now), 10)+note)
		}
		for _, undo := range database.Undos {
			lines = append(lines, "  "+pad("undo-"+undo.ID, 30)+pad(human.Ago(undo.At, now), 10)+style.Muted.Render("before a restore"))
		}
	}
	return lines
}

// backupChip is how the backups of an app are, in a few words for the
// list.
func (m *model) backupChip(key appKey) string {
	known := m.facts[key]
	switch {
	case known == nil || !known.loaded:
		return style.Warning.Render(m.spinner())
	case known.err != "":
		return style.Badge(style.StatusFailed, "unknown")
	case len(known.backups) == 0:
		return style.Muted.Render("no database")
	}
	worst := ""
	for _, database := range known.backups {
		state := backupState(database, time.Now())
		if worst == "" || strings.Contains(state, style.SymbolFailed) || (strings.Contains(state, "!") && !strings.Contains(worst, style.SymbolFailed)) {
			worst = state
		}
	}
	return worst
}

// backupState is how one database is backed up, in a few words.
func backupState(database protocol.DatabaseBackup, now time.Time) string {
	switch {
	case database.Error != "" || database.CheckError != "":
		return style.Badge(style.StatusFailed, "check failed")
	case len(database.Copies) == 0:
		return style.Warning.Render("! no copy yet")
	case database.CheckedAt == nil:
		return style.Muted.Render(fmt.Sprintf("%s, newest %s", count(len(database.Copies), "copy"), human.Ago(database.Copies[0].At, now)))
	}
	return style.Success.Render(style.SymbolDone+" checked "+human.Ago(*database.CheckedAt, now)) + style.Muted.Render(", "+count(len(database.Copies), "copy"))
}

// checkState says how the last daily restore check went.
func checkState(database protocol.DatabaseBackup, now time.Time) string {
	switch {
	case database.CheckedAt == nil:
		return style.Muted.Render("No check yet. Once a day the newest copy is restored and read.")
	case database.CheckError != "":
		return failed("Checked " + human.Ago(*database.CheckedAt, now) + ": " + database.CheckError)
	}
	return succeeded("Checked " + human.Ago(*database.CheckedAt, now) + ": the copy marked checked restored fine")
}

// serverNeeds are what on a server needs you, each with the command
// that settles it: what the server says, and port 22 open to the
// internet.
func serverNeeds(server server) []daemon.Need {
	needs := slices.Clone(server.status.Needs)
	if server.status.PublicSSH {
		needs = append(needs, daemon.Need{Text: server.name + ": port 22 is open to the internet.", Command: build.Name + " secure " + server.name})
	}
	return needs
}

// needs are what on a server needs you, an app that does not run too.
func (m *model) needs(server server) []daemon.Need {
	needs := serverNeeds(server)
	for _, app := range server.status.Apps {
		if !app.Running {
			needs = append(needs, daemon.Need{Text: app.Name + " does not run.", Command: build.Name + " logs " + app.Name})
		}
	}
	return needs
}

// needLines are needs as lines: the text in amber, the command under it.
func needLines(needs []daemon.Need) []string {
	var lines []string
	for _, need := range needs {
		lines = append(lines, "", style.Warning.Render("! ")+need.Text, "  "+style.Accent.Render(need.Command))
	}
	return lines
}

// logsBox is the logs of the open app, as many of the newest lines as
// fit.
func (m *model) logsBox(rows int) []string {
	app := m.app
	state := style.Success.Render(style.SymbolLive) + style.Muted.Render(" following")
	if app.logsEnded != "" {
		state = style.Muted.Render(app.logsEnded)
	}
	title := app.key.app + style.Muted.Render(" on "+app.key.server+" · logs")
	logs := app.logs[max(len(app.logs)-(rows-3), 0):]
	lines := []string{state, ""}
	if len(logs) == 0 && app.logsEnded == "" {
		lines = append(lines, style.Warning.Render(m.spinner())+style.Muted.Render(" waiting for the first line"))
	}
	for _, line := range logs {
		lines = append(lines, style.AppLog(line))
	}
	return box(title, lines, m.width, rows)
}

// overlay is the question, the picker or the help, floating over the
// body, or "" when none is open.
func (m *model) overlay() string {
	width := min(m.width-4, 72)
	switch {
	case m.question != nil:
		lines := wrap(m.question.text, width-4)
		lines = append(lines, "", style.Bold.Render("y")+style.Muted.Render(" yes    ")+style.Bold.Render("n")+style.Muted.Render(" or ")+style.Bold.Render("esc")+style.Muted.Render(" no"))
		return strings.Join(box(m.question.title, lines, width, len(lines)+2), "\n")
	case m.picker != nil:
		return strings.Join(m.pickerBox(width), "\n")
	case m.help:
		lines := helpLines()
		return strings.Join(box("Keys", lines, width, len(lines)+2), "\n")
	}
	return ""
}

// pickerBox is a picker as a box, the picked row marked.
func (m *model) pickerBox(width int) []string {
	p := m.picker
	nameWidth := 0
	for _, row := range p.rows {
		nameWidth = max(nameWidth, lipgloss.Width(row.name))
	}
	room := min(len(p.rows), max(m.height-10, 3))
	first := max(p.cursor-room+1, 0)
	lines := []string{}
	for index, row := range p.rows[first : first+room] {
		text := pad(row.name, nameWidth) + "   " + pad(row.when, 12) + row.note
		lines = append(lines, m.row(first+index == p.cursor, text).text)
	}
	return box(p.title, lines, width, len(lines)+2)
}

// statusLine is what runs now, or the last result.
func (m *model) statusLine() string {
	if m.action == nil {
		return " " + m.message
	}
	text := " " + style.Warning.Render(m.spinner()) + " " + m.action.what
	if m.action.step != "" {
		text += style.Muted.Render(" · " + m.action.step)
	}
	if took := time.Since(m.action.since); took >= time.Second {
		text += style.Muted.Render(" · " + human.Duration(took))
	}
	return text
}

// keys are the keys that work now: the key bold, what it does muted.
func (m *model) keys() string {
	var pairs []string
	switch {
	case m.question != nil:
		pairs = []string{"y", "yes", "n", "no"}
	case m.picker != nil:
		pairs = []string{"↑↓", "select", "enter", "pick", "esc", "cancel"}
	case m.help:
		pairs = []string{"any key", "closes the help"}
	case m.app != nil:
		pairs = []string{"esc", "back", "r", "restart", "b", "rollback", "o", "open", "q", "quit"}
	case m.tab == serversTab:
		pairs = []string{"↑↓", "select", "enter", "its apps", "tab", "next", "?", "help", "q", "quit"}
	case m.tab == backupsTab:
		pairs = []string{"↑↓", "select", "enter", "restore", "c", "copy now", "tab", "next", "?", "help", "q", "quit"}
	default:
		pairs = []string{"↑↓", "select", "enter", "logs", "r", "restart", "b", "rollback", "o", "open", "tab", "next", "?", "help", "q", "quit"}
		if _, ok := m.current(); !ok {
			pairs = []string{"tab", "next", "?", "help", "q", "quit"}
		}
	}
	parts := []string{}
	for index := 0; index < len(pairs); index += 2 {
		parts = append(parts, style.Bold.Render(pairs[index])+" "+style.Muted.Render(pairs[index+1]))
	}
	return " " + strings.Join(parts, "  ")
}

// helpLines say what each key does.
func helpLines() []string {
	keys := [][2]string{
		{"↑ ↓  j k", "select"},
		{"tab  1 2 3", "apps, servers, backups"},
		{"enter", "an app's logs, a server's apps, a restore"},
		{"esc", "back from the logs"},
		{"r", "restart the app"},
		{"b", "go back to an older deploy"},
		{"c", "copy the app's databases now"},
		{"R", "put a database back to a copy"},
		{"o", "open the app's domain in your browser"},
		{"q", "quit"},
	}
	lines := []string{}
	for _, key := range keys {
		lines = append(lines, style.Bold.Render(pad(key[0], 12))+style.Muted.Render(key[1]))
	}
	return append(lines, "",
		style.Muted.Render("Each step that changes a server asks first."),
		style.Muted.Render("Every server is asked again every "+human.Duration(refreshEvery)+"."),
		style.Muted.Render("Deploy from the app's folder: ")+style.Accent.Render(build.Name+" deploy"),
		style.Muted.Render("Everything else: ")+style.Accent.Render(build.Name+" help"))
}

// box draws lines in a frame with round corners and the title in its
// top edge, width × height cells with the frame. Lines that do not fit
// are cut.
func box(title string, lines []string, width, height int) []string {
	edge := style.Dim
	inner := max(width-4, 1)
	title = fit(title, max(width-6, 1))
	top := edge.Render("╭─ ") + style.Bold.Render(title) + edge.Render(" "+strings.Repeat("─", max(width-5-lipgloss.Width(title), 0))+"╮")
	out := []string{top}
	for index := range max(height-2, 0) {
		line := ""
		if index < len(lines) {
			line = fit(lines[index], inner)
		}
		out = append(out, edge.Render("│")+" "+pad(line, inner)+" "+edge.Render("│"))
	}
	return append(out, edge.Render("╰"+strings.Repeat("─", max(width-2, 0))+"╯"))
}

// fact is one fact of the side: a muted label in a column, the value.
func fact(label, value string) string {
	return style.Muted.Render(pad(label, 10)) + value
}

// wrap breaks text into lines of at most width cells, at spaces.
func wrap(text string, width int) []string {
	return strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n")
}

// spinner is the running symbol, turning.
func (m *model) spinner() string {
	return style.Spinner[m.frame%len(style.Spinner)]
}

// count is a number and its noun, like "1 server" or "3 apps".
func count(n int, noun string) string {
	if n != 1 {
		noun += "s"
		if strings.HasSuffix(noun, "ys") {
			noun = strings.TrimSuffix(noun, "ys") + "ies"
		}
	}
	return strconv.Itoa(n) + " " + noun
}

// appState is how an app runs, as in the CLI's table.
func appState(running bool) string {
	if running {
		return style.Badge(style.StatusLive, "running")
	}
	return style.Badge(style.StatusIdle, "stopped")
}

// pad fills text with spaces to width cells.
func pad(text string, width int) string {
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}

// fit cuts a line to width cells, so it never wraps.
func fit(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}
