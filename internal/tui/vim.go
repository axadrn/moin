package tui

// The vim keys: counts, motions, the g, c and Z keys, search, the command
// line and help. They run before the keys of a tab and work the same in
// every list, in the output panel and in help.

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// vim takes the keys that move, search, open the command line or help. It
// gives back the key for the tab to handle, "" when it took it.
func (m model) vim(key string) (model, tea.Cmd, string) {
	count := m.count
	if pending := m.pending; pending != "" {
		m.pending, m.count = "", 0
		switch pending + key {
		case "gg":
			if count > 0 {
				m, cmd := m.motion("line", count)
				return m, cmd, ""
			}
			m, cmd := m.motion("top", 1)
			return m, cmd, ""
		case "gt":
			if count > 0 {
				m, cmd := m.toTab(tab(min(count, tabs) - 1))
				return m, cmd, ""
			}
			m, cmd := m.toTab((m.tab + 1) % tabs)
			return m, cmd, ""
		case "gT":
			m, cmd := m.toTab((m.tab + tabs - 1) % tabs)
			return m, cmd, ""
		case "ZZ", "ZQ":
			return m, tea.Quit, ""
		case "gh", "cw":
			if m.mode == modeList {
				return m, nil, pending + key
			}
		}
		// Like vim, a pair it does not know does nothing.
		return m, nil, ""
	}
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && (key != "0" || count > 0) {
		m.count = min(count*10+int(key[0]-'0'), 9999)
		return m, nil, ""
	}
	m.count = 0
	switch {
	case key == "g" || key == "Z":
		m.pending, m.count = key, count
		return m, nil, ""
	case key == "c" && (m.tab == tabServers || m.tab == tabTeam) && m.mode == modeList:
		m.pending = key
		return m, nil, ""
	case key == "esc" && count > 0:
		return m, nil, ""
	}
	step := max(count, 1)

	if m.mode == modeHelp {
		switch key {
		case "esc", "q", "?", "h":
			m.mode = modeList
		case "j", "down", "k", "up", "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b", "pgdown", "pgup", "G", "home", "end":
			m, cmd := m.motion(motionOf(key), step)
			return m, cmd, ""
		}
		return m, nil, ""
	}

	switch key {
	case "j", "down", "k", "up", "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b", "pgdown", "pgup", "home", "end":
		m, cmd := m.motion(motionOf(key), step)
		return m, cmd, ""
	case "G":
		if count > 0 {
			m, cmd := m.motion("line", count)
			return m, cmd, ""
		}
		m, cmd := m.motion("bottom", 1)
		return m, cmd, ""
	case "/":
		m.mode, m.input = modeSearch, ""
		return m, nil, ""
	case ":":
		m.mode, m.input = modeCommand, ""
		return m, nil, ""
	case "?":
		m.mode, m.helpTop = modeHelp, 0
		return m, nil, ""
	case "n", "N":
		m, cmd := m.find(key == "N")
		return m, cmd, ""
	case "shift+tab":
		m, cmd := m.toTab((m.tab + tabs - 1) % tabs)
		return m, cmd, ""
	case "ctrl+w":
		// Like moving between vim windows: the list and the output panel.
		if m.tab == tabApps && m.outputTitle != "" {
			m.panelFocus = !m.panelFocus
		}
		return m, nil, ""
	case "esc", "h":
		if m.panelFocused() {
			m.panelFocus = false
			return m, nil, ""
		}
		if key == "h" {
			// Back, like esc, but only out of an open app.
			if m.tab == tabApps && m.detail != nil {
				return m, nil, "esc"
			}
			return m, nil, ""
		}
	case "l":
		// Into: the app, then the log of the picked deploy.
		if m.tab == tabApps {
			return m, nil, "enter"
		}
		return m, nil, ""
	}
	return m, nil, key
}

// motionOf names what a key does to the place in view.
func motionOf(key string) string {
	switch key {
	case "j", "down":
		return "down"
	case "k", "up":
		return "up"
	case "ctrl+d":
		return "halfdown"
	case "ctrl+u":
		return "halfup"
	case "ctrl+f", "pgdown":
		return "pagedown"
	case "ctrl+b", "pgup":
		return "pageup"
	case "home":
		return "top"
	}
	return "bottom"
}

// motion moves in what has the keys: help, the output panel or the list.
// count is how far, or for "line" the line to go to, from 1.
func (m model) motion(kind string, count int) (model, tea.Cmd) {
	switch {
	case m.mode == modeHelp:
		room := m.bodyRoom()
		last := max(len(helpLines())-room, 0)
		top := m.helpTop
		switch kind {
		case "down", "up":
			top += sign(kind == "down") * count
		case "halfdown", "halfup":
			top += sign(kind == "halfdown") * max(room/2, 1) * count
		case "pagedown", "pageup":
			top += sign(kind == "pagedown") * room * count
		case "top":
			top = 0
		case "bottom":
			top = last
		case "line":
			top = count - 1
		}
		m.helpTop = min(max(top, 0), last)
		return m, nil
	case m.panelFocused():
		// The panel scrolls from its end: outputScroll lines are below it.
		lines, _ := m.panelLines()
		keep := m.panelKeep()
		last := max(len(lines)-keep, 0)
		scroll := min(m.outputScroll, last)
		switch kind {
		case "down", "up":
			scroll -= sign(kind == "down") * count
		case "halfdown", "halfup":
			scroll -= sign(kind == "halfdown") * max(keep/2, 1) * count
		case "pagedown", "pageup":
			scroll -= sign(kind == "pagedown") * keep * count
		case "top":
			scroll = last
		case "bottom":
			scroll = 0
		case "line":
			scroll = len(lines) - count - keep/2
		}
		m.outputScroll = min(max(scroll, 0), last)
		return m, nil
	}
	at, _, length := m.list()
	switch kind {
	case "down", "up":
		return m.moveTo(at + sign(kind == "down")*count)
	case "halfdown", "halfup":
		return m.moveTo(at + sign(kind == "halfdown")*max(m.bodyRoom()/2, 1)*count)
	case "pagedown", "pageup":
		return m.moveTo(at + sign(kind == "pagedown")*m.bodyRoom()*count)
	case "top":
		return m.moveTo(0)
	case "line":
		return m.moveTo(count - 1)
	}
	return m.moveTo(length - 1)
}

func sign(positive bool) int {
	if positive {
		return 1
	}
	return -1
}

// list is the list the keys move in: the selected row, the lowest row
// there is (-1 in an open app: no deploy picked) and how many rows.
func (m model) list() (at, low, length int) {
	switch m.tab {
	case tabServers:
		return m.selected, 0, len(m.servers)
	case tabActivity:
		return m.activitySelected, 0, len(m.events)
	case tabTeam:
		return m.teamSelected, 0, len(m.teamRows())
	case tabSettings:
		return m.settingSelected, 0, len(m.settingRows())
	}
	if m.detail != nil {
		deploys := m.pickable()
		return slices.IndexFunc(deploys, func(deploy client.Deploy) bool { return deploy.Number == m.deployPick }), -1, len(deploys)
	}
	return m.appSelected, 0, len(m.apps)
}

// pickable are the deploys of the open app that can be picked.
func (m model) pickable() []client.Deploy {
	return m.detail.Deploys[:min(len(m.detail.Deploys), historyRows)]
}

// moveTo selects a row of the list, as near to index as there is.
func (m model) moveTo(index int) (model, tea.Cmd) {
	at, low, length := m.list()
	if length == 0 {
		return m, nil
	}
	index = min(max(index, low), length-1)
	switch m.tab {
	case tabServers:
		m.selected = index
	case tabActivity:
		m.activitySelected = index
		// Going to the end of what is loaded loads the older events.
		if index == length-1 && index > at && m.activityNext != "" {
			return m, m.loadOlder()
		}
	case tabTeam:
		m.teamSelected = index
	case tabSettings:
		m.settingSelected = index
	default:
		if m.detail != nil {
			m.deployPick = 0
			if index >= 0 {
				m.deployPick = m.pickable()[index].Number
			}
			return m, nil
		}
		// The panel belongs to the app it was opened for; a deploy
		// streams on.
		if index != m.appSelected && !m.running {
			m = m.closePanel()
		}
		m.appSelected = index
	}
	return m, nil
}

// rowTexts is what search looks at in each row of the list.
func (m model) rowTexts() []string {
	var rows []string
	switch m.tab {
	case tabServers:
		for _, server := range m.servers {
			rows = append(rows, server.Name+" "+server.System()+" "+server.AgentVersion+" "+server.Access())
		}
	case tabActivity:
		for _, event := range m.events {
			rows = append(rows, event.Actor+" "+event.Via+" "+event.Summary)
		}
	case tabTeam:
		for _, row := range m.teamRows() {
			rows = append(rows, strings.Join(row.cells, " "))
		}
	case tabSettings:
		for _, row := range m.settingRows() {
			rows = append(rows, row.name)
		}
	default:
		if m.detail != nil {
			for _, deploy := range m.pickable() {
				rows = append(rows, fmt.Sprintf("v%d %s %s %s %s", deploy.Number, deploy.Status, deploy.Commit, deploy.CommitMessage, deploy.Error))
			}
			break
		}
		for _, app := range m.apps {
			rows = append(rows, app.Name+" "+app.ServerName)
		}
	}
	return rows
}

// matches reports whether text holds pattern. Case does not count unless
// the pattern has a capital letter, like vim's smartcase.
func matches(text, pattern string) bool {
	if pattern == strings.ToLower(pattern) {
		text = strings.ToLower(text)
	}
	return strings.Contains(text, pattern)
}

// find goes to the next row or panel line that matches the search, the one
// before with back, and goes on at the other end like vim.
func (m model) find(back bool) (model, tea.Cmd) {
	if m.search == "" {
		m.err = errors.New("E35: no previous search; type / and a word first")
		return m, nil
	}
	var texts []string
	at := 0
	panel := m.panelFocused()
	if panel {
		lines, raw := m.panelLines()
		for index := range lines {
			texts = append(texts, m.output[raw[index]])
		}
		// From the last match, or from the bottom line in view.
		at = m.panelMatch - 1
		if at < 0 {
			at = len(lines) - 1 - min(m.outputScroll, max(len(lines)-m.panelKeep(), 0))
		}
	} else {
		texts = m.rowTexts()
		at, _, _ = m.list()
	}
	for step := 1; step <= len(texts); step++ {
		index := at + sign(!back)*step
		wrapped := index < 0 || index >= len(texts)
		index = (index%len(texts) + len(texts)) % len(texts)
		if !matches(texts[index], m.search) {
			continue
		}
		if wrapped && at >= 0 {
			m.notice = "search hit BOTTOM, continuing at TOP"
			if back {
				m.notice = "search hit TOP, continuing at BOTTOM"
			}
		}
		if !panel {
			return m.moveTo(index)
		}
		m.panelMatch = index + 1
		keep := m.panelKeep()
		last := max(len(texts)-keep, 0)
		// The match goes to the middle unless it is in view already.
		if bottom := len(texts) - 1 - min(m.outputScroll, last); index > bottom || index <= bottom-keep {
			m.outputScroll = min(max(len(texts)-1-index-keep/2, 0), last)
		}
		return m, nil
	}
	m.err = fmt.Errorf("E486: pattern not found: %s", m.search)
	return m, nil
}

// panelFocused reports whether the keys scroll the output panel.
func (m model) panelFocused() bool {
	return m.panelFocus && m.tab == tabApps && m.outputTitle != ""
}

// panelKeep is how many lines the output panel showed last time.
func (m model) panelKeep() int {
	if m.shared != nil && m.shared.panelKeep > 0 {
		return m.shared.panelKeep
	}
	return 10
}

// bodyRoom is how many lines the body has between header and footer.
func (m model) bodyRoom() int {
	if m.height <= 0 {
		return 20
	}
	width := m.width
	if width <= 0 {
		width = 100
	}
	return max(m.height-len(m.header(width))-len(m.footer(width))-1, 1)
}

// toTab switches to a tab and loads what it shows.
func (m model) toTab(to tab) (model, tea.Cmd) {
	m.tab = to
	m.added, m.removed = nil, nil
	switch to {
	case tabActivity:
		return m, m.loadActivity()
	case tabTeam:
		return m, m.loadTeam()
	case tabSettings:
		return m, m.loadSettings()
	}
	return m, nil
}

// lineKey types into the search or the command line.
func (m model) lineKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key := msg.String(); key {
	case "esc":
		m.mode, m.input = modeList, ""
	case "enter":
		input := strings.TrimSpace(m.input)
		search := m.mode == modeSearch
		m.mode, m.input = modeList, ""
		if !search {
			return m.command(input)
		}
		// An empty search looks for the last one again, like vim.
		if input != "" {
			m.search, m.panelMatch = input, 0
		}
		return m.find(false)
	case "tab":
		if m.mode == modeCommand {
			m = m.complete()
		}
	case "backspace":
		// Backspace on nothing closes the line, like vim.
		if m.input == "" {
			m.mode = modeList
		}
		m.input = dropLast(m.input)
	default:
		m.input += msg.Text
	}
	return m, nil
}

// commandKeys are the commands that do what a key of a tab does, by tab.
var commandKeys = map[string]map[tab]string{
	"add":      {tabServers: "a", tabApps: "a", tabSettings: "a"},
	"backups":  {tabApps: "b"},
	"deploy":   {tabApps: "d"},
	"domains":  {tabApps: "o"},
	"env":      {tabApps: "e"},
	"filter":   {tabActivity: "f"},
	"firewall": {tabServers: "f"},
	"github":   {tabApps: "gh"},
	"hook":     {tabApps: "hook"},
	"image":    {tabApps: "i"},
	"install":  {tabServers: "i"},
	"invite":   {tabTeam: "a"},
	"logs":     {tabApps: "L"},
	"move":     {tabServers: "J", tabApps: "J"},
	"older":    {tabActivity: "m"},
	"protect":  {tabApps: "p"},
	"redeploy": {tabApps: "R"},
	"remove":   {tabServers: "x", tabApps: "x", tabSettings: "x"},
	"rename":   {tabServers: "cw", tabTeam: "cw"},
	"restart":  {tabApps: "t"},
	"rollback": {tabApps: "r"},
	"sql":      {tabApps: "s"},
	"ssh":      {tabServers: "s"},
	"start":    {tabApps: "S"},
	"stop":     {tabApps: "S"},
	"tunnel":   {tabServers: "t"},
	"update":   {tabServers: "u", tabSettings: "u"},
	"uptime":   {tabApps: "u"},
	"metrics":  {tabServers: "m", tabApps: "m"},
	// auto-update turns automatic agent updates on or off.
	"auto-update": {tabSettings: "A"},
}

// tabNames are the tabs in their order, as commands.
var tabNames = []string{"servers", "apps", "activity", "team", "settings"}

// commandNames are what tab completes on the command line.
func commandNames() []string {
	names := append([]string{"e", "help", "noh", "open", "q", "tabnext", "tabprevious"}, tabNames...)
	for name := range commandKeys {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// complete fills in the command typed so far, as far as it is clear, and
// lists the ones it could be.
func (m model) complete() model {
	if strings.Contains(m.input, " ") {
		return m
	}
	var found []string
	for _, name := range commandNames() {
		if strings.HasPrefix(name, m.input) {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return m
	}
	common := found[0]
	for _, name := range found[1:] {
		for !strings.HasPrefix(name, common) {
			common = common[:len(common)-1]
		}
	}
	m.input = common
	if len(found) == 1 {
		m.input += " "
	} else {
		m.notice = strings.Join(found, "  ")
	}
	return m
}

// command runs what was typed on the command line.
func (m model) command(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return m, nil
	}
	name, args := fields[0], fields[1:]
	if number, err := strconv.Atoi(name); err == nil {
		return m.motion("line", max(number, 1))
	}
	switch name {
	case "q", "q!", "qa", "qa!", "quit", "wq", "x", "xa":
		return m, tea.Quit
	case "h", "help":
		m.mode, m.helpTop = modeHelp, 0
		return m, nil
	case "noh", "nohlsearch":
		m.search, m.panelMatch = "", 0
		return m, nil
	case "tabn", "tabnext":
		return m.toTab((m.tab + 1) % tabs)
	case "tabp", "tabprevious", "tabN", "tabNext":
		return m.toTab((m.tab + tabs - 1) % tabs)
	case "e", "edit", "open":
		if len(args) == 0 {
			m.err = errors.New("E32: name the app: :e <app>")
			return m, nil
		}
		index := slices.IndexFunc(m.apps, func(app client.App) bool { return app.Name == args[0] })
		if index < 0 {
			m.err = fmt.Errorf("there is no app %s", args[0])
			return m, nil
		}
		m.tab, m.appSelected, m.detail, m.deployPick, m.panelFocus = tabApps, index, nil, 0, false
		m = m.closePanel()
		return m, m.loadDetail(args[0])
	}
	if index := slices.Index(tabNames, name); index >= 0 {
		return m.toTab(tab(index))
	}
	keys, ok := commandKeys[name]
	if !ok {
		m.err = fmt.Errorf("E492: not a command: %s. Press tab to complete, ? for help", name)
		return m, nil
	}
	key, ok := keys[m.tab]
	if !ok {
		var where []string
		for index, tabName := range tabNames {
			if _, ok := keys[tab(index)]; ok {
				where = append(where, tabName)
			}
		}
		m.err = fmt.Errorf(":%s works on the %s tab", name, strings.Join(where, " and "))
		return m, nil
	}
	// :move up is K, :move down J.
	if name == "move" {
		switch {
		case len(args) > 0 && args[0] == "up":
			key = "K"
		case len(args) > 0 && args[0] != "down":
			m.err = errors.New("move it up or down: :move up, :move down")
			return m, nil
		}
	}
	if app, ok := m.currentApp(); ok && m.tab == tabApps {
		switch {
		case name == "stop" && app.Stopped:
			m.notice = app.Name + " is stopped already. :start starts it"
			return m, nil
		case name == "start" && !app.Stopped:
			m.notice = app.Name + " runs already"
			return m, nil
		case name == "hook" && len(args) > 0 && args[0] == "off":
			key = "hook off"
		}
	}
	next, cmd := m.tabKey(key)
	// :rename shop fills in the new name, to check and send with enter.
	if opened := next.(model); name == "rename" && len(args) > 0 && opened.mode == modeForm {
		opened.input = strings.Join(args, " ")
		return opened, cmd
	}
	return next, cmd
}

// dropWord takes the last word off text, like ctrl+w in a shell.
func dropWord(text string) string {
	text = strings.TrimRight(text, " ")
	return text[:strings.LastIndex(text, " ")+1]
}

// helpEntries are the keys help lists; an entry without keys is a heading.
var helpEntries = [][2]string{
	{"", "Move"},
	{"j k  ↓ ↑", "down, up; 5j moves five"},
	{"gg G", "first, last; 5G or :5 goes to row 5"},
	{"ctrl+d ctrl+u", "half a page down, up"},
	{"ctrl+f ctrl+b", "a page down, up"},
	{"l enter", "into: the app, the log of the picked deploy"},
	{"h esc", "back; esc never quits"},
	{"tab gt", "next tab; 3gt goes to the third"},
	{"shift+tab gT", "previous tab"},
	{"", "Search"},
	{"/word", "finds a row or a log line; case counts with a capital"},
	{"n N", "next, previous match"},
	{":noh", "forgets the search"},
	{"", "Command line"},
	{":q ZZ", "quit, like q and ctrl+c"},
	{":e <app>", "opens an app"},
	{":servers :apps …", "switches the tab"},
	{":deploy :restart …", "every key by its name, like :logs or :hook off"},
	{"tab", "completes a command"},
	{"", "Output panel"},
	{"L", "shows the logs, ready to scroll"},
	{"ctrl+w", "moves between the list and the panel"},
	{"j k  ctrl+d ctrl+u", "scroll"},
	{"gg G", "the top, the end; G follows new lines"},
	{"", "Apps"},
	{"a", "create an app"},
	{"d", "deploy the directory ops started in"},
	{"gh", "GitHub: deploy a branch, link or unlink a repo"},
	{"i", "deploy an image"},
	{"R", "redeploy"},
	{"t", "restart"},
	{"S", "stop, start"},
	{"r", "roll back"},
	{"e", "env vars"},
	{"s", "SQL"},
	{"b", "backups and restores"},
	{"u", "uptime: pause, resume, a path per domain"},
	{"m", "metrics: CPU, memory and network, live"},
	{"o", "domains"},
	{"p", "password"},
	{":hook", "a new deploy hook; :hook off turns it off"},
	{"K J", "move the app up, down; :move up, :move down"},
	{"x", "remove"},
	{"", "Servers"},
	{"a", "add a server"},
	{"cw", "rename"},
	{"t", "Cloudflare Tunnel on, off"},
	{"f", "firewall on, off"},
	{"s", "SSH only on Tailscale"},
	{"i", "a new install command"},
	{"u", "update the agent"},
	{"m", "metrics: CPU, memory, disk, network, the apps by memory, live"},
	{"K J", "move the server up, down; :move up, :move down"},
	{"x", "remove"},
	{"", "Settings"},
	{"enter", "change the setting"},
	{"u :update", "update a self-hosted control plane"},
	{"A :auto-update", "agents update by themselves, on or off"},
	{"a", "add a registry login"},
	{"x", "remove"},
	{"", "Inputs"},
	{"ctrl+u", "clears the line"},
	{"ctrl+w", "deletes the last word"},
	{"esc", "cancels"},
}

// helpLines is help as lines of the body.
func helpLines() []string {
	width := 0
	for _, entry := range helpEntries {
		width = max(width, len([]rune(entry[0])))
	}
	var lines []string
	for _, entry := range helpEntries {
		if entry[0] == "" {
			if lines != nil {
				lines = append(lines, "")
			}
			lines = append(lines, pad+style.Bold.Render(entry[1]))
			continue
		}
		lines = append(lines, pad+"  "+style.Accent.Render(entry[0])+strings.Repeat(" ", width-len([]rune(entry[0]))+3)+style.Muted.Render(entry[1]))
	}
	return lines
}

// helpView is the part of help that fits.
func (m model) helpView(room int) []string {
	lines := helpLines()
	if room < 0 {
		return lines
	}
	top := min(m.helpTop, max(len(lines)-room, 0))
	return lines[top:min(top+room, len(lines))]
}
