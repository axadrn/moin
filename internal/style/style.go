// Package style is the look of ops in the terminal: the brand and status
// colors of the web UI (ops-cloud DESIGN.md), symbols and text styles. The
// TUI and the CLI both use it, so a status looks the same everywhere.
package style

import (
	"charm.land/lipgloss/v2"
)

// Colors. The web UI uses the same hues in oklch.
var (
	Brand   = lipgloss.Color("#FF6B2C")
	Live    = lipgloss.Color("#3FCF7A")
	Running = lipgloss.Color("#F2B544")
	Failed  = lipgloss.Color("#FF5A4E")
	Idle    = lipgloss.Color("#8A8A8A")
	Faint   = lipgloss.Color("#5E5E5E")
)

// Symbols. Status symbols always come with a word, never alone.
const (
	SymbolLive    = "●"
	SymbolRunning = "◐"
	SymbolFailed  = "✗"
	SymbolIdle    = "○"
	SymbolDone    = "✓"
	SymbolStep    = "→"
)

// Text styles.
var (
	Bold      = lipgloss.NewStyle().Bold(true)
	Muted     = lipgloss.NewStyle().Foreground(Idle)
	Dim       = lipgloss.NewStyle().Foreground(Faint)
	Accent    = lipgloss.NewStyle().Foreground(Brand)
	AccentBox = lipgloss.NewStyle().Foreground(lipgloss.Color("#141414")).Background(Brand).Bold(true).Padding(0, 1)
	Success   = lipgloss.NewStyle().Foreground(Live)
	Warning   = lipgloss.NewStyle().Foreground(Running)
	Danger    = lipgloss.NewStyle().Foreground(Failed)
)

// Status kinds map every status word of servers, apps and deploys to one look.
type Status int

const (
	StatusIdle Status = iota
	StatusLive
	StatusRunning
	StatusFailed
)

// StatusOf maps a status word to its kind: online, live and succeeded are
// live; running, deploying and waiting are running; failed and offline are
// failed; everything else is idle.
func StatusOf(word string) Status {
	switch word {
	case "online", "live", "succeeded":
		return StatusLive
	case "running", "deploying", "waiting":
		return StatusRunning
	case "failed", "offline":
		return StatusFailed
	}
	return StatusIdle
}

// Badge renders a status as its colored symbol and text, like "● live v3".
func Badge(status Status, text string) string {
	switch status {
	case StatusLive:
		return Success.Render(SymbolLive + " " + text)
	case StatusRunning:
		return Warning.Render(SymbolRunning + " " + text)
	case StatusFailed:
		return Danger.Render(SymbolFailed + " " + text)
	}
	return Muted.Render(SymbolIdle + " " + text)
}
