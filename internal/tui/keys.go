package tui

import (
	tea "charm.land/bubbletea/v2"
)

// pressed handles one key. A question or the release picker takes the
// keys while it is open; then the help; then the screen.
func (m *model) pressed(key string) tea.Cmd {
	if key == "ctrl+c" {
		return m.quit()
	}
	switch {
	case m.question != nil:
		return m.answer(key)
	case m.picker != nil:
		return m.pick(key)
	case m.help:
		m.help = false
		return nil
	}
	switch key {
	case "q":
		return m.quit()
	case "?":
		m.help = true
	case "up", "k":
		if m.app == nil {
			m.move(-1)
		}
	case "down", "j":
		if m.app == nil {
			m.move(1)
		}
	case "enter":
		if key, ok := m.current(); ok && m.app == nil {
			return m.openApp(key)
		}
	case "esc", "backspace", "left":
		m.closeApp()
	case "r":
		return m.askRestart()
	case "b":
		return m.askReleases()
	case "o":
		return m.openAddress()
	}
	return nil
}

// quit stops the logs and ends the terminal UI.
func (m *model) quit() tea.Cmd {
	m.closeApp()
	return tea.Quit
}
