// Package style is the look of moin in the terminal: the brand and status
// colors, symbols and text styles, so a status looks the same everywhere.
package style

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Colors carry status only; the brand is monochrome, like the web UI. The
// accent is bold text in the terminal's own foreground color, so it works
// on light and dark terminals.
var (
	Live    = lipgloss.Color("#3FCF7A")
	Running = lipgloss.Color("#F2B544")
	Failed  = lipgloss.Color("#FF5A4E")
	Idle    = lipgloss.Color("#8A8A8A")
	Faint   = lipgloss.Color("#5E5E5E")
	Ink     = lipgloss.Color("#141414")
)

// Symbols. Status symbols always come with a word, never alone.
const (
	SymbolLive    = "●"
	SymbolRunning = "◐"
	SymbolFailed  = "✗"
	SymbolIdle    = "○"
	SymbolDone    = "✓"
	SymbolStep    = "→"
	// SymbolSelected marks the selected row of a list.
	SymbolSelected = "▌"
)

// Spinner is the running symbol turning, for waits on a terminal.
var Spinner = []string{"◐", "◓", "◑", "◒"}

// Text styles.
var (
	Bold    = lipgloss.NewStyle().Bold(true)
	Muted   = lipgloss.NewStyle().Foreground(Idle)
	Dim     = lipgloss.NewStyle().Foreground(Faint)
	Accent  = lipgloss.NewStyle().Bold(true)
	Mark    = lipgloss.NewStyle().Reverse(true).Bold(true).Padding(0, 1)
	Success = lipgloss.NewStyle().Foreground(Live)
	Warning = lipgloss.NewStyle().Foreground(Running)
	Danger  = lipgloss.NewStyle().Foreground(Failed)
)

// Status kinds map every status word of servers, apps and deploys to one look.
type Status int

const (
	StatusIdle Status = iota
	StatusLive
	StatusRunning
	StatusFailed
	// StatusWaiting is a server whose daemon never connected: hollow, in the
	// running color.
	StatusWaiting
	// StatusDone is a deploy that succeeded but is no longer live.
	StatusDone
)

// StatusOf maps a status word to its kind: online, live and succeeded are
// live; running and deploying are running; waiting waits; failed and offline
// are failed; everything else is idle.
func StatusOf(word string) Status {
	switch word {
	case "online", "live", "succeeded":
		return StatusLive
	case "running", "deploying":
		return StatusRunning
	case "waiting":
		return StatusWaiting
	case "failed", "offline":
		return StatusFailed
	}
	return StatusIdle
}

// Symbol is the symbol of a status kind.
func Symbol(status Status) string {
	switch status {
	case StatusLive:
		return SymbolLive
	case StatusRunning:
		return SymbolRunning
	case StatusFailed:
		return SymbolFailed
	case StatusDone:
		return SymbolDone
	}
	return SymbolIdle
}

// Of is the text style of a status kind.
func Of(status Status) lipgloss.Style {
	switch status {
	case StatusLive:
		return Success
	case StatusRunning, StatusWaiting:
		return Warning
	case StatusFailed:
		return Danger
	}
	return Muted
}

// Badge renders a status as its colored symbol and text, like "● live v3".
func Badge(status Status, text string) string {
	return Of(status).Render(Symbol(status) + " " + text)
}

// steps are how the daemon's own deploy log lines start (logf in
// internal/daemon/apps.go); every other line is tool output.
var steps = []string{"downloading the source", "pulling ", "detected ", "building", "starting", "rolling back to ", "database ", "health check ", "deploy failed: "}

// LogLine sorts a deploy log line. Steps are what the daemon says ("building",
// "health check …"); everything else is tool output such as docker build
// lines. text is the line without the indent docker adds.
func LogLine(line string) (text string, step bool) {
	text = strings.TrimRight(line, " \t\r")
	for _, prefix := range steps {
		if strings.HasPrefix(text, prefix) {
			return text, true
		}
	}
	if number, ok := strings.CutSuffix(text, " is live"); ok && strings.HasPrefix(number, "v") {
		return text, true
	}
	return strings.TrimLeft(text, " \t"), false
}

// Noise renders tool output in a deploy log: dimmed, errors in the failed
// color so they stand out of the build noise.
func Noise(text string) string {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		return Danger.Render(text)
	}
	return Dim.Render(text)
}

// Step renders a step of the daemon: the arrow muted, the text plain, a
// failure in the failed color.
func Step(text string) string {
	if strings.HasPrefix(text, "deploy failed") {
		return Danger.Render(SymbolFailed + " " + text)
	}
	return Muted.Render(SymbolStep) + " " + text
}
