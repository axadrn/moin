package tui

// The mouse: the wheel scrolls what the arrows move, a click on a tab
// opens it. Hold shift to select text, as in other full-screen apps.

import (
	tea "charm.land/bubbletea/v2"
)

// tabBarY is the line of the tab names: the header, the app line and a
// blank one above it.
const tabBarY = 4

func (m model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.auth != nil || m.ask != nil || m.prompt != nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		code := tea.KeyDown
		switch msg.Button {
		case tea.MouseWheelUp:
			code = tea.KeyUp
		case tea.MouseWheelDown:
		default:
			return m, nil
		}
		// text scrolls three lines a notch, a list one row
		steps := 1
		if !m.layered() && (m.cur.v == vDeploy || m.cur.v == vExec || (m.cur.v == vApp && m.cur.tab == tLogs)) {
			steps = 3
		}
		var next tea.Model = m
		var cmds []tea.Cmd
		for range steps {
			var cmd tea.Cmd
			next, cmd = next.(model).key(tea.KeyPressMsg{Code: code})
			cmds = append(cmds, cmd)
		}
		return next, tea.Batch(cmds...)
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.layered() || m.cur.v != vApp || msg.Y != tabBarY {
			return m, nil
		}
		spans, _ := tabSpans(m.w - 2*margin)
		for _, sp := range spans {
			if x := msg.X - margin; x >= sp.x0 && x < sp.x1 {
				return m.switchTab(sp.t)
			}
		}
	}
	return m, nil
}
