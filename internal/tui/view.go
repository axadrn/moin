package tui

// The frame every view sits in: the header with where you are, the body,
// the footer with the keys of the place, and the layers over it.

import (
	"fmt"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
)

const margin = 2

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = build.Name + "."
	return v
}

// render draws the whole screen, exactly m.w by m.h.
func (m model) render() string {
	u := ui{p: m.pal}
	w, h := m.w, m.h
	if w < 60 || h < 16 {
		return strings.Join(block(center(u.muted(build.Name+". needs at least 60 by 16"), w), w, h), "\n")
	}
	if m.auth != nil {
		return strings.Join(m.authView(u, w, h), "\n")
	}
	head := m.header(u)
	foot := m.footer(u)
	bh := h - len(head) - len(foot)
	cw := w - 2*margin
	body := block(m.body(u, cw, bh), cw, bh)
	for i := range body {
		body[i] = strings.Repeat(" ", margin) + body[i] + strings.Repeat(" ", margin)
	}
	f := ui{p: m.pal, float: true}
	if m.layered() {
		for i := range body {
			body[i] = u.faint(escapes.ReplaceAllString(body[i], ""))
		}
	}
	switch {
	case m.ask != nil:
		pw := min(68, w-8)
		pop := m.confirmView(f, pw)
		body = overlay(body, pop, (w-pw)/2, max(1, (bh-len(lines(pop)))/2))
	case m.restore != nil:
		pw := min(76, w-8)
		pop := m.restoreView(f, pw)
		body = overlay(body, pop, (w-pw)/2, max(1, (bh-len(lines(pop)))/2))
	case m.prompt != nil:
		pw := min(76, w-8)
		pop := m.promptView(f, pw)
		body = overlay(body, pop, (w-pw)/2, max(1, bh-len(lines(pop))-1))
	case m.pick != nil:
		pw := min(88, w-8)
		pop := m.pickerView(f, pw, min(22, bh-2))
		body = overlay(body, pop, (w-pw)/2, max(1, (bh-len(lines(pop)))/3))
	case m.menuOn:
		pop := m.menuView(f, cw)
		body = overlay(body, pop, margin, max(0, bh-len(lines(pop))))
	case m.help:
		pw := min(96, w-6)
		pop := m.helpView(f, pw, bh-2)
		body = overlay(body, pop, (w-pw)/2, 1)
	}
	all := append(append(head, body...), foot...)
	for i := range all {
		all[i] = padR(all[i], w)
	}
	return strings.Join(all[:h], "\n")
}

// layered is whether a layer sits over the view.
func (m model) layered() bool {
	return m.ask != nil || m.restore != nil || m.prompt != nil || m.pick != nil || m.menuOn || m.help
}

func (m model) crumbs() []string {
	ws := m.ws.name
	if ws == "" {
		ws = "workspace"
	}
	c := []string{ws}
	switch m.cur.v {
	case vDash:
		c = append(c, "dashboard")
	case vApp:
		c = append(c, m.cur.app, tabName(m.cur.tab))
	case vDeploy:
		c = append(c, m.cur.app, fmt.Sprintf("v%d", m.cur.deploy))
	case vExec:
		c = append(c, m.cur.app, "run")
	case vServers:
		c = append(c, "servers")
	case vServer:
		c = append(c, "servers", m.cur.server)
	case vActivity:
		c = append(c, "activity")
	case vWeb:
		c = append(c, "web app")
	}
	return c
}

func (m model) header(u ui) []string {
	var parts []string
	cr := m.crumbs()
	for i, c := range cr {
		if i == len(cr)-1 {
			parts = append(parts, u.strong(c))
		} else {
			parts = append(parts, u.muted(c))
		}
	}
	l := "  " + u.strong("moin.") + "   " + strings.Join(parts, u.faint(sep))
	conn := u.dot("live", m.frame%4 != 3) + " " + u.muted("live")
	switch {
	case m.offline != nil:
		conn = u.bad(dot + " reconnecting")
	case !m.loaded && m.cached:
		conn = u.warn(spinner[m.frame%len(spinner)]) + " " + u.muted("last seen · connecting")
	case !m.loaded:
		conn = u.warn(spinner[m.frame%len(spinner)]) + " " + u.muted("connecting")
	}
	host := m.url
	if p, err := url.Parse(m.url); err == nil && p.Host != "" {
		host = p.Host
	}
	r := conn + u.faint("  ·  ") + u.muted(host) + "  "
	if wid(l)+wid(r) > m.w {
		r = conn + "  "
	}
	return []string{spread(l, r, m.w), ""}
}

func (m model) footer(u ui) []string {
	w := m.w
	right := u.keys(kb{"space", "menu"}, kb{"ctrl+k", "search"}, kb{"?", "keys"})
	l := u.keys(m.viewKeys()...)
	if m.notice != "" {
		mark := u.good(dot)
		if m.noticeErr {
			mark = u.bad(dot)
		}
		l = mark + " " + u.text(m.notice)
		if m.noticeErr {
			l = mark + " " + u.bad(m.notice)
		}
	}
	if m.layered() || m.cur.v == vExec {
		right = ""
	}
	if wid(l)+wid(right)+6 > w {
		right = u.keys(kb{"space", "menu"}, kb{"?", "keys"})
	}
	if wid(l)+wid(right)+6 > w {
		l = truncate(l, w-wid(right)-6)
	}
	return []string{"", spread("  "+l, right+"  ", w)}
}

// viewKeys are the hints of the place, the most useful first.
func (m model) viewKeys() []kb {
	switch {
	case m.pick != nil:
		return []kb{{"↑↓", "move"}, {"enter", "open"}, {"esc", "close"}}
	case m.menuOn:
		return []kb{{"key", "choose"}, {"backspace", "up"}, {"esc", "close"}}
	case m.ask != nil && m.ask.name != "":
		return []kb{{"type", "the name"}, {"enter", "confirm"}, {"esc", "cancel"}}
	case m.ask != nil:
		return []kb{{"y", m.ask.verb}, {"esc", "cancel"}}
	case m.restore != nil:
		return []kb{{"←→", "hour"}, {"H L", "day"}, {"↑↓", "minute"}, {"enter", "restore"}, {"esc", "cancel"}}
	case m.prompt != nil:
		return []kb{{"enter", "save"}, {"esc", "cancel"}}
	case m.help:
		return []kb{{"esc", "close"}}
	}
	switch m.cur.v {
	case vDash:
		return []kb{{"↑↓", "select"}, {"tab", "panel"}, {"enter", "open"}, {"s", "servers"}, {"a", "activity"}, {"q", "quit"}}
	case vApp:
		ks := []kb{{"d l b e o u m", "views"}}
		switch m.cur.tab {
		case tDeploys:
			ks = append(ks, kb{"enter", "log"}, kb{"r", "roll back"}, kb{"R", "redeploy"})
		case tLogs:
			p := "pause"
			if m.logs.paused {
				p = "resume"
			}
			ks = append(ks, kb{"/", "filter"}, kb{"p", p}, kb{"G", "follow"})
		case tBackups:
			ks = append(ks, kb{"N", "back up now"}, kb{"enter", "restore"}, kb{"t", "point in time"})
		case tEnv:
			v := "show values"
			if m.shown {
				v = "hide values"
			}
			ks = append(ks, kb{"a", "set"}, kb{"x", "remove"}, kb{"v", v}, kb{"E", "editor"})
		case tDomains:
			ks = append(ks, kb{"a", "add"}, kb{"x", "remove"}, kb{"c", "canonical"}, kb{"p", "password"})
		case tUptime:
			ks = append(ks, kb{"p", "pause"}, kb{"c", "path"}, kb{"r", "check"})
		case tMetrics:
			ks = append(ks, kb{"[ ]", "range"})
		}
		return append(ks, kb{"esc", "back"})
	case vDeploy:
		if m.out.running {
			return []kb{{"↑↓", "scroll"}, {"G", "follow"}, {"esc", "back"}}
		}
		return []kb{{"↑↓", "scroll"}, {"r", "roll back to it"}, {"o", "app"}, {"esc", "back"}}
	case vExec:
		return []kb{{"enter", "run"}, {"@service", "picks one"}, {"↑↓", "scroll"}, {"esc", "back"}}
	case vServers:
		return []kb{{"↑↓", "select"}, {"enter", "open"}, {"u", "update agent"}, {"esc", "back"}}
	case vServer:
		return []kb{{"[ ]", "range"}, {"enter", "app"}, {"u", "update agent"}, {"W", "web"}, {"esc", "back"}}
	case vActivity:
		return []kb{{"↑↓", "scroll"}, {"enter", "open"}, {"esc", "back"}}
	case vWeb:
		return []kb{{"↑↓", "select"}, {"enter", "open in the browser"}, {"esc", "back"}}
	}
	return nil
}

func (m model) body(u ui, w, h int) string {
	switch m.cur.v {
	case vApp:
		return m.appView(u, w, h)
	case vDeploy, vExec:
		return m.outView(u, w, h)
	case vServers:
		return m.serversView(u, w, h)
	case vServer:
		return m.serverView(u, w, h)
	case vActivity:
		return m.activityView(u, w, h)
	case vWeb:
		return m.webView(u, w, h)
	}
	return m.dashboard(u, w, h)
}

// appState is the status word and label of an app: live v3, deploying v4,
// failed v4, stopped, new.
func appState(a client.App) (string, string) {
	switch {
	case a.LatestNumber == 0:
		return "new", "new"
	case a.LatestStatus == "running":
		return "deploying", fmt.Sprintf("deploying v%d", a.LatestNumber)
	case a.Stopped:
		return "stopped", "stopped"
	case a.LatestStatus == "failed":
		return "failed", fmt.Sprintf("failed v%d", a.LatestNumber)
	}
	return "live", fmt.Sprintf("live v%d", a.LiveNumber)
}

// float paints an overlay on its own background.
func (u ui) floating(s string, w int) string { return fill(s, u.p.Raised, w) }

func (m model) menuView(u ui, w int) string {
	title, groups := m.menuGroups()
	iw := inner(w)
	kw := 3
	for _, g := range groups {
		for _, it := range g.items {
			kw = max(kw, wid(it.key))
		}
	}
	colw := 24
	if len(groups) > 1 {
		colw = max(20, min(28, (iw-3*(len(groups)-1))/len(groups)))
	} else {
		colw = iw
	}
	var cols []string
	for _, g := range groups {
		col := []string{u.faint(g.name), ""}
		for _, it := range g.items {
			label := u.text(it.label)
			if it.sub != "" {
				label = u.text(it.label) + u.faint(" ›")
			}
			col = append(col, padR(u.strong(padL(it.key, kw))+"  "+label, colw))
		}
		cols = append(cols, strings.Join(col, "\n"))
	}
	// as many groups side by side as fit, the rest below
	per := max(1, (iw+3)/(colw+3))
	var rows []string
	for i := 0; i < len(cols); i += per {
		rows = append(rows, hjoin(3, cols[i:min(i+per, len(cols))]...))
		if i+per < len(cols) {
			rows = append(rows, "")
		}
	}
	path := "space"
	if title != "" {
		path += sep + title
	}
	right := u.faint("esc close")
	if title != "" {
		right = u.faint("backspace up · esc close")
	}
	body := append([]string{""}, lines(strings.Join(rows, "\n"))...)
	body = append(body, "")
	return u.floating(u.panel(path, right, body, w, len(body)+chrome, true), w)
}

func (m model) pickerView(u ui, w, maxH int) string {
	iw := inner(w)
	hs := m.hits()
	caret := u.strong("▏")
	q := u.strong(m.pick.query) + caret
	if m.pick.query == "" {
		q = caret + u.faint("apps, servers, views, actions …")
	}
	input := spread(u.strong("›")+" "+q, u.faint(fmt.Sprintf("%d", len(hs))), iw)
	body := []string{input, u.rule(iw)}
	n := max(1, maxH-chrome-4)
	start := 0
	if m.pick.at >= n {
		start = m.pick.at - n + 1
	}
	tw := min(36, iw/2)
	for i := start; i < len(hs) && i < start+n; i++ {
		h := hs[i]
		sel := i == m.pick.at
		body = append(body, u.row([]cell{{u.faint(h.e.kind), 8, false}, {highlight(u, h.e.title, h.pos, sel), tw, false}, {u.muted(h.e.hint), 0, false}}, iw, sel))
	}
	if len(hs) == 0 {
		body = append(body, "", u.muted("Nothing matches “"+m.pick.query+"”"))
	}
	for len(body) < maxH-chrome-2 {
		body = append(body, "")
	}
	body = append(body, u.rule(iw), u.keys(kb{"↑↓", "move"}, kb{"enter", "open"}, kb{"esc", "close"}))
	return u.floating(u.panel("Search", "", body, w, len(body)+chrome, true), w)
}

func highlight(u ui, s string, pos []int, sel bool) string {
	set := map[int]bool{}
	for _, p := range pos {
		set[p] = true
	}
	base := sty().Foreground(u.p.Muted)
	if sel {
		base = sty().Foreground(u.p.Fg)
	}
	hi := sty().Foreground(u.p.Fg).Bold(true).Underline(true)
	var b strings.Builder
	for i, r := range []rune(s) {
		if set[i] {
			b.WriteString(hi.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
}

func (m model) confirmView(u ui, w int) string {
	a := m.ask
	iw := inner(w)
	body := []string{""}
	for _, l := range wrap(a.detail, iw) {
		body = append(body, u.muted(l))
	}
	body = append(body, "")
	if a.name != "" {
		typed := u.strong(a.typed) + u.strong("▏")
		if a.typed == "" {
			typed = u.strong("▏") + u.faint(a.name)
		}
		body = append(body, u.muted("Type ")+u.text(a.name)+u.muted(" to confirm"), "", u.strong("›")+" "+typed, "")
		ok := a.typed == a.name
		k := u.faint("enter " + a.verb)
		if ok {
			k = u.bad("enter " + a.verb)
		}
		body = append(body, k+u.faint("  ·  esc cancel"))
	} else {
		verb := u.strong("y " + a.verb)
		if a.danger {
			verb = u.bad("y " + a.verb)
		}
		body = append(body, verb+u.faint("  ·  esc cancel"))
	}
	return u.floating(u.panel(a.title, "", body, w, len(body)+chrome+1, true), w)
}

func (m model) promptView(u ui, w int) string {
	p := m.prompt
	iw := inner(w)
	value := u.strong(p.value) + u.strong("▏")
	body := []string{u.muted(truncate(p.hint, iw)), "", u.strong("›") + " " + truncate(value, iw-2), "", u.keys(kb{"enter", "save"}, kb{"esc", "cancel"})}
	return u.floating(u.panel(p.title, "", body, w, len(body)+chrome, true), w)
}

func (m model) restoreView(u ui, w int) string {
	r := m.restore
	iw := inner(w)
	now := m.now()
	from, to := r.window(now)
	bar := iw - 2
	total := to.Sub(from)
	pos := bar - 1
	if total > 0 {
		pos = int(float64(r.at.Sub(from)) / float64(total) * float64(bar-1))
	}
	var b strings.Builder
	for i := range bar {
		switch {
		case i == pos:
			b.WriteString(u.strong("┃"))
		case bar >= 12 && i%(bar/6) == 0:
			b.WriteString(u.faint("┆"))
		default:
			b.WriteString(u.fg(u.p.Line, "─"))
		}
	}
	var dbs []string
	for i, d := range r.dbs {
		if i == r.db {
			dbs = append(dbs, u.strong(d.Database))
		} else {
			dbs = append(dbs, u.muted(d.Database))
		}
	}
	dbLine := strings.Join(dbs, u.faint("  ·  "))
	if len(r.dbs) > 1 {
		dbLine += u.faint("   tab switches")
	}
	var body []string
	for _, l := range wrap("Pick a moment. The app stops for the swap and starts again with the database as it was then.", iw) {
		body = append(body, u.muted(l))
	}
	body = append(body,
		"",
		" "+b.String(),
		" "+spread(u.faint(from.Local().Format("Jan 2 15:04")), u.faint(ago(to, now)), bar),
		"",
		spread(u.muted("database"), dbLine, iw),
		spread(u.muted("restore to"), u.strong(r.at.Local().Format("Mon Jan 2 · 15:04:05")), iw),
		spread(u.muted(""), u.faint(ago(r.at, now)), iw),
		"",
	)
	if r.typing {
		typed := u.strong(r.typed) + u.strong("▏")
		if r.typed == "" {
			typed = u.strong("▏") + u.faint(r.app)
		}
		body = append(body, u.muted("Type ")+u.text(r.app)+u.muted(" to restore. This cannot be undone."), u.strong("›")+" "+typed)
	} else {
		body = append(body, u.keys(kb{"←→", "hour"}, kb{"H L", "day"}, kb{"↑↓", "minute"}, kb{"enter", "restore"}, kb{"esc", "cancel"}))
	}
	return u.floating(u.panel("Restore "+r.app+" to a point in time", "", body, w, len(body)+chrome, true), w)
}

// help lists the keys that work everywhere and the ones of this place.
func (m model) helpView(u ui, w, maxH int) string {
	iw := inner(w)
	global := []kb{
		{"space", "the menu"},
		{"space space", "search"},
		{"ctrl+k /", "search"},
		{"esc", "back one level"},
		{"q", "quit, or back"},
		{"?", "these keys"},
		{"↑↓ j k", "move"},
		{"g G", "first, last"},
		{"ctrl+c", "quit from anywhere"},
	}
	m.help = false
	here := m.viewKeys()
	if m.cur.v == vApp {
		for _, a := range m.appActions(m.cur.app) {
			here = append(here, kb{a.key, a.label})
		}
	}
	col := func(title string, ks []kb, cw int) string {
		kw := 8
		for _, k := range ks {
			kw = max(kw, min(wid(k.k)+2, 16))
		}
		out := []string{u.faint(title), ""}
		for _, k := range ks {
			out = append(out, padR(u.strong(padR(k.k, kw))+u.muted(k.l), cw))
		}
		return strings.Join(out, "\n")
	}
	cw := (iw - 4) / 2
	var body []string
	if iw >= 60 {
		body = lines(hjoin(4, col("Everywhere", global, cw), col("Here", here, cw)))
	} else {
		body = append(lines(col("Everywhere", global, iw)), "")
		body = append(body, lines(col("Here", here, iw))...)
	}
	body = append([]string{""}, body...)
	if len(body) > maxH-chrome-1 {
		body = body[:max(0, maxH-chrome-1)]
	}
	body = append(body, "")
	return u.floating(u.panel("Keys", u.faint("esc close"), body, w, len(body)+chrome, true), w)
}
