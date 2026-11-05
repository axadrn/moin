package tui

// The which-key menu on `space` and the jump list on `ctrl+k`.

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

type menuItem struct {
	key, label string
	sub        string // opens this submenu
	do         func(m model) (tea.Model, tea.Cmd)
}

type menuGroup struct {
	name  string
	items []menuItem
}

// menuGroups are the groups of the menu level open now.
func (m model) menuGroups() (string, []menuGroup) {
	if len(m.menu) > 0 {
		switch m.menu[0] {
		case "a":
			var items []menuItem
			names := make([]string, len(m.apps))
			for i, a := range m.apps {
				names[i] = a.Name
			}
			keys := assignKeys(names)
			nw := 0
			for _, n := range names {
				nw = max(nw, wid(n))
			}
			for _, a := range m.apps {
				name := a.Name
				w, label := appState(a)
				items = append(items, menuItem{key: keys[name], label: padR(name, nw+2) + (ui{p: m.pal}).chip(w, label), do: func(m model) (tea.Model, tea.Cmd) { return m.openApp(name, tDeploys) }})
			}
			return "apps", []menuGroup{{"Open an app", items}}
		case "w":
			var items []menuItem
			for _, p := range m.webPages() {
				path := p.path
				items = append(items, menuItem{key: p.key, label: p.name, do: func(m model) (tea.Model, tea.Cmd) { return m.browse(path) }})
			}
			return "web", []menuGroup{{"Open in the browser", items}}
		}
	}
	goTo := menuGroup{"Go to", []menuItem{
		{key: "h", label: "dashboard", do: func(m model) (tea.Model, tea.Cmd) { return m.home() }},
		{key: "a", label: "apps", sub: "a"},
		{key: "s", label: "servers", do: func(m model) (tea.Model, tea.Cmd) { return m.open(screen{v: vServers}) }},
		{key: "v", label: "activity", do: func(m model) (tea.Model, tea.Cmd) { return m.open(screen{v: vActivity}) }},
		{key: "w", label: "web app", sub: "w"},
		{key: "k", label: "jump to …", do: func(m model) (tea.Model, tea.Cmd) { return m.openPicker(), nil }},
	}}
	groups := []menuGroup{goTo}
	if app := m.cur.app; app != "" {
		var views []menuItem
		for _, t := range tabs {
			t := t
			views = append(views, menuItem{key: string(t.t), label: t.name, do: func(m model) (tea.Model, tea.Cmd) { return m.openApp(app, t.t) }})
		}
		var do []menuItem
		for _, a := range m.appActions(app) {
			do = append(do, menuItem{key: a.key, label: a.label, do: a.do})
		}
		groups = append(groups, menuGroup{app, views}, menuGroup{"Do", do})
	}
	if m.cur.v == vServer {
		if sv, ok := m.findServer(m.cur.server); ok {
			groups = append(groups, menuGroup{sv.Name, []menuItem{
				{key: "u", label: "update the agent", do: func(m model) (tea.Model, tea.Cmd) { return m.confirmAgentUpdate(sv) }},
				{key: "W", label: "security in the web", do: func(m model) (tea.Model, tea.Cmd) { return m.browse("/servers/" + sv.ID + "/security") }},
			}})
		}
	}
	goTo.items = append(goTo.items, menuItem{key: "?", label: "all keys", do: func(m model) (tea.Model, tea.Cmd) { m.help = true; return m, nil }})
	groups[0] = goTo
	return "", groups
}

func (m model) menuKey(s string) (tea.Model, tea.Cmd) {
	switch s {
	case "esc", "space":
		m.menuOn, m.menu = false, nil
		return m, nil
	case "backspace":
		if len(m.menu) > 0 {
			m.menu = m.menu[:len(m.menu)-1]
		} else {
			m.menuOn = false
		}
		return m, nil
	}
	_, groups := m.menuGroups()
	for _, g := range groups {
		for _, it := range g.items {
			if it.key != s {
				continue
			}
			if it.sub != "" {
				m.menu = append(m.menu, it.sub)
				return m, nil
			}
			m.menuOn, m.menu = false, nil
			return it.do(m)
		}
	}
	return m, nil
}

// assignKeys gives every name one key: its first letter when free, else
// another of its letters, else a digit.
func assignKeys(names []string) map[string]string {
	used := map[string]bool{}
	out := map[string]string{}
	for _, n := range names {
		for _, r := range strings.ToLower(n) {
			k := string(r)
			if (unicode.IsLetter(r) || unicode.IsDigit(r)) && !used[k] {
				out[n], used[k] = k, true
				break
			}
		}
	}
	d := 1
	for _, n := range names {
		if out[n] != "" {
			continue
		}
		for used[fmt.Sprint(d%10)] && d < 10 {
			d++
		}
		out[n] = fmt.Sprint(d % 10)
		used[out[n]] = true
	}
	return out
}

// picker is the jump list: a query over every app, server, view and action.
type picker struct {
	query string
	at    int
}

type entry struct {
	kind, title, hint string
	do                func(m model) (tea.Model, tea.Cmd)
}

func (m model) openPicker() model {
	m.pick = &picker{}
	m.menuOn, m.menu = false, nil
	return m
}

// entries are everything the jump list can reach.
func (m model) entries() []entry {
	var out []entry
	for _, a := range m.apps {
		name := a.Name
		_, label := appState(a)
		out = append(out, entry{"app", name, label + " · " + a.ServerName, func(m model) (tea.Model, tea.Cmd) { return m.openApp(name, tDeploys) }})
	}
	for _, s := range m.servers {
		name := s.Name
		out = append(out, entry{"server", name, s.Status() + " · " + s.System(), func(m model) (tea.Model, tea.Cmd) { return m.open(screen{v: vServer, server: name}) }})
	}
	out = append(out,
		entry{"view", "dashboard", "what runs and what needs you", func(m model) (tea.Model, tea.Cmd) { return m.home() }},
		entry{"view", "servers", "every server and its agent", func(m model) (tea.Model, tea.Cmd) { return m.open(screen{v: vServers}) }},
		entry{"view", "activity", "who did what", func(m model) (tea.Model, tea.Cmd) { return m.open(screen{v: vActivity}) }},
		entry{"view", "web app", "setup that wants a browser", func(m model) (tea.Model, tea.Cmd) { return m.open(screen{v: vWeb}) }},
	)
	for _, a := range m.apps {
		name := a.Name
		for _, t := range tabs {
			t := t
			out = append(out, entry{t.name, name + " " + t.name, "", func(m model) (tea.Model, tea.Cmd) { return m.openApp(name, t.t) }})
		}
		for _, ac := range m.appActions(name) {
			ac := ac
			out = append(out, entry{"action", ac.label + " " + name, "key " + ac.key + " in the app", func(m model) (tea.Model, tea.Cmd) {
				m2, cmd := m.openApp(name, m.tabOr(name))
				next, cmd2 := ac.do(m2)
				return next, tea.Batch(cmd, cmd2)
			}})
		}
	}
	for _, p := range m.webPages() {
		path := p.path
		out = append(out, entry{"web", p.name, p.why, func(m model) (tea.Model, tea.Cmd) { return m.browse(path) }})
	}
	return out
}

// tabOr is the tab an app shows now, or deploys.
func (m model) tabOr(app string) tab {
	if m.cur.v == vApp && m.cur.app == app {
		return m.cur.tab
	}
	return tDeploys
}

type hit struct {
	e     entry
	score int
	pos   []int
}

// fuzzy scores q as a subsequence of s: runs and word starts win. pos are
// the rune positions that matched.
func fuzzy(q, s string) (int, []int, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return 0, nil, true
	}
	rs := []rune(strings.ToLower(s))
	var pos []int
	score, prev, j := 0, -2, 0
	qr := []rune(q)
	for i := 0; i < len(rs) && j < len(qr); i++ {
		if qr[j] == ' ' {
			j++
			if j >= len(qr) {
				break
			}
		}
		if rs[i] != qr[j] {
			continue
		}
		pos = append(pos, i)
		score++
		if i == prev+1 {
			score += 5
		}
		if i == 0 || strings.ContainsRune(" -_./", rs[i-1]) {
			score += 8
		}
		prev = i
		j++
	}
	for j < len(qr) && qr[j] == ' ' {
		j++
	}
	if j < len(qr) {
		return 0, nil, false
	}
	if strings.Contains(string(rs), q) {
		score += 25
	}
	if strings.HasPrefix(string(rs), q) {
		score += 10
	}
	score -= len(rs) / 4
	return score, pos, true
}

// hits are the entries that match the query, the best first.
func (m model) hits() []hit {
	q := ""
	if m.pick != nil {
		q = m.pick.query
	}
	var out []hit
	for _, e := range m.entries() {
		if q == "" && e.kind != "app" && e.kind != "server" && e.kind != "view" {
			continue
		}
		sc, pos, ok := fuzzy(q, e.title)
		if !ok {
			continue
		}
		out = append(out, hit{e, sc, pos})
	}
	if q != "" {
		sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	}
	return out
}

func (m model) pickKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := *m.pick
	switch k.String() {
	case "esc", "ctrl+k":
		m.pick = nil
		return m, nil
	case "enter":
		hs := m.hits()
		m.pick = nil
		if p.at < len(hs) {
			return hs[p.at].e.do(m)
		}
		return m, nil
	case "down", "ctrl+n", "ctrl+j", "tab":
		if p.at < len(m.hits())-1 {
			p.at++
		}
	case "up", "ctrl+p", "shift+tab":
		if p.at > 0 {
			p.at--
		}
	default:
		q, ok := edit(p.query, k)
		if ok {
			p.query, p.at = q, 0
		}
	}
	m.pick = &p
	return m, nil
}
