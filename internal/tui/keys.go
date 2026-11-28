package tui

import (
	tea "charm.land/bubbletea/v2"
)

// pressed handles one key. A question or a picker takes the keys while
// it is open; then the help; then the logs or the lists.
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
		return nil
	case "esc", "backspace", "left", "h":
		m.closeApp()
		return nil
	}
	if m.app != nil {
		return m.actionKey(key)
	}
	switch key {
	case "tab", "right", "l":
		return m.switchTab((m.tab + 1) % 3)
	case "shift+tab":
		return m.switchTab((m.tab + 2) % 3)
	case "1", "2", "3":
		return m.switchTab(tab(key[0] - '1'))
	case "up", "k":
		m.move(-1)
		return m.askFacts()
	case "down", "j":
		m.move(1)
		return m.askFacts()
	case "enter":
		return m.enter()
	}
	return m.actionKey(key)
}

// actionKey handles the keys that act on the current app.
func (m *model) actionKey(key string) tea.Cmd {
	switch key {
	case "r":
		return m.askRestart()
	case "b":
		return m.askRollback()
	case "o":
		return m.openAddress()
	case "c":
		return m.askBackup()
	case "R":
		return m.askRestore()
	}
	return nil
}

// enter opens what the selected row leads to: an app's logs, a
// server's apps, the copies to restore.
func (m *model) enter() tea.Cmd {
	switch m.tab {
	case serversTab:
		for _, key := range m.apps() {
			if key.server == m.selectedServer {
				m.selected = key
				return m.switchTab(appsTab)
			}
		}
		return nil
	case backupsTab:
		return m.askRestore()
	}
	if key, ok := m.current(); ok {
		return m.openApp(key)
	}
	return nil
}

// switchTab shows another tab and asks for what it shows.
func (m *model) switchTab(to tab) tea.Cmd {
	m.tab = to
	return m.askFacts()
}

// quit stops the logs and ends the terminal UI.
func (m *model) quit() tea.Cmd {
	m.closeApp()
	return tea.Quit
}
