package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// olderEventsMsg is the next page of the activity, older than what shows.
type olderEventsMsg client.ActivityPage

// activityPage is how many events the activity tab loads at a time.
const activityPage = 100

// loadActivity fetches what the team did, with the filter; every change of
// the servers or apps loads it again while the tab is open.
func (m model) loadActivity() tea.Cmd {
	query := m.activityFilter
	query.Limit = activityPage
	return func() tea.Msg {
		page, err := m.api.Activity(m.ctx, query)
		if err != nil {
			return errMsg{err}
		}
		return eventsMsg(page)
	}
}

// loadOlder fetches the page after the last event shown.
func (m model) loadOlder() tea.Cmd {
	query := m.activityFilter
	query.Limit, query.Cursor = activityPage, m.activityNext
	return func() tea.Msg {
		page, err := m.api.Activity(m.ctx, query)
		if err != nil {
			return errMsg{err}
		}
		return olderEventsMsg(page)
	}
}

// activityKey filters the activity with f and loads older events with m.
func (m model) activityKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "f":
		filter := m.activityFilter
		return m.ask("Filter the activity", func(values []string) tea.Cmd {
			return func() tea.Msg { return activityFilterMsg(client.ActivityQuery{App: values[0], Server: values[1], User: values[2]}) }
		},
			formField{label: "App", hint: "empty for every app", value: filter.App, optional: true},
			formField{label: "Server", hint: "empty for every server", value: filter.Server, optional: true},
			formField{label: "Member", hint: "an email, empty for everyone", value: filter.User, optional: true},
		), nil
	case "c":
		m.activityFilter = client.ActivityQuery{}
		return m, m.loadActivity()
	case "m":
		if m.activityNext != "" {
			return m, m.loadOlder()
		}
	}
	return m, nil
}

// activityFilterMsg is a new filter for the activity.
type activityFilterMsg client.ActivityQuery

func (m model) activityKeys() [][2]string {
	keys := [][2]string{{"tab", "team"}, {"f", "filter"}}
	if m.activityFilter != (client.ActivityQuery{}) {
		keys = append(keys, [2]string{"c", "clear filter"})
	}
	if m.activityNext != "" {
		keys = append(keys, [2]string{"m", "older"})
	}
	return append(keys, [2]string{"q", "quit"})
}

// activityView is who did what, newest first, to the second in the local
// time zone, as many as fit.
func (m model) activityView(room int) []string {
	var lines []string
	if filter := activityFilterText(m.activityFilter); filter != "" {
		lines = append(lines, pad+style.Muted.Render("Only "+filter+". Press c to see everything."))
		room--
	}
	if len(m.events) == 0 {
		return append(lines, pad+style.Muted.Render("Nothing happened yet. Deploys, restores, SQL writes and every other change show up here."))
	}
	rows := make([][]string, 0, len(m.events))
	for _, event := range m.events {
		rows = append(rows, []string{style.Muted.Render(event.CreatedAt.Local().Format("2006-01-02 15:04:05 MST")), event.Actor, style.Muted.Render(viaName(event.Via, event.Token)), event.Summary})
	}
	return append(lines, table([]string{"WHEN", "WHO", "VIA", "WHAT"}, rows, -1, room)...)
}

// activityFilterText says what the filter keeps: "app blog, member ada@…".
func activityFilterText(filter client.ActivityQuery) string {
	parts := []string{}
	for _, part := range [][2]string{{"app", filter.App}, {"server", filter.Server}, {"member", filter.User}} {
		if part[1] != "" {
			parts = append(parts, part[0]+" "+part[1])
		}
	}
	return strings.Join(parts, ", ")
}

// viaName names how a change came in, with the API token it came with.
func viaName(via, token string) string {
	switch via {
	case "hook":
		via = "deploy hook"
	case "system":
		via = "ops"
	}
	if token != "" {
		return via + ", token " + token
	}
	return via
}
