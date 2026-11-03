package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// K and J move the selected server or app up or down in the order of the
// workspace, which every list shows. The list changes at once; the live
// stream brings the saved order back.

// moveServer moves the selected server one up or down.
func (m model) moveServer(up bool) (tea.Model, tea.Cmd) {
	to, ok := neighbour(m.selected, len(m.servers), up)
	if !ok {
		return m, nil
	}
	m.servers = moved(m.servers, m.selected, to)
	m.selected = to
	order := make([]string, 0, len(m.servers))
	for _, server := range m.servers {
		order = append(order, server.ID)
	}
	return m, func() tea.Msg {
		if _, err := m.api.ReorderServers(m.ctx, order); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// moveApp moves the selected app one up or down.
func (m model) moveApp(up bool) (tea.Model, tea.Cmd) {
	to, ok := neighbour(m.appSelected, len(m.apps), up)
	if !ok {
		return m, nil
	}
	m.apps = moved(m.apps, m.appSelected, to)
	m.appSelected = to
	order := make([]string, 0, len(m.apps))
	for _, app := range m.apps {
		order = append(order, app.Name)
	}
	return m, func() tea.Msg {
		if _, err := m.api.ReorderApps(m.ctx, order); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// neighbour is the row above or below at, if there is one.
func neighbour(at, length int, up bool) (int, bool) {
	to := at + 1
	if up {
		to = at - 1
	}
	return to, at < length && to >= 0 && to < length
}

// moved is a copy of items with the one at from moved to to.
func moved[T any](items []T, from, to int) []T {
	item := items[from]
	rest := slices.Delete(slices.Clone(items), from, from+1)
	return slices.Insert(rest, to, item)
}
