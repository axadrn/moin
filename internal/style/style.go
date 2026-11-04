// Package style is the look of ops in the terminal: the brand and status
// colors of the web UI (controlplane/DESIGN.md), symbols and text styles. The
// TUI and the CLI both use it, so a status looks the same everywhere.
package style

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/axadrn/moin/internal/client"
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
	// StatusWaiting is a server whose agent never connected: hollow, in the
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

// Server is the status badge of a server: ● online, ○ waiting, ✗ offline.
func Server(server client.Server) string {
	word := server.Status()
	return Badge(StatusOf(word), word)
}

// App is the status badge of an app: ● live v3, ◐ deploying v4,
// ✗ failed v4 · v3 live, ○ new.
func App(app client.App) string {
	switch {
	case app.LatestNumber == 0:
		return Badge(StatusIdle, "new")
	case app.LatestStatus == "running":
		return Badge(StatusRunning, fmt.Sprintf("deploying v%d", app.LatestNumber))
	case app.Stopped:
		return Badge(StatusIdle, fmt.Sprintf("stopped v%d", app.LiveNumber))
	case app.LatestStatus == "failed" && app.LiveNumber > 0:
		return Badge(StatusFailed, fmt.Sprintf("failed v%d", app.LatestNumber)) + Muted.Render(fmt.Sprintf(" · v%d live", app.LiveNumber))
	case app.LatestStatus == "failed":
		return Badge(StatusFailed, fmt.Sprintf("failed v%d", app.LatestNumber))
	}
	return Badge(StatusLive, fmt.Sprintf("live v%d", app.LiveNumber))
}

// Deploy is the status badge of one deploy of an app: the live one is
// ● live, older good ones ✓ succeeded.
func Deploy(deploy client.Deploy, liveNumber int) string {
	switch {
	case deploy.Status == "succeeded" && deploy.Number == liveNumber:
		return Badge(StatusLive, "live")
	case deploy.Status == "succeeded":
		return Badge(StatusDone, "succeeded")
	}
	return Badge(StatusOf(deploy.Status), deploy.Status)
}

// Duration is "850ms", "16s", "2m 5s" or "3h 10m".
func Duration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// steps are how the agent's own deploy log lines start (logf in
// internal/agent/apps.go); every other line is tool output.
var steps = []string{"downloading the source", "pulling ", "detected ", "building", "starting", "rolling back to ", "database ", "health check ", "deploy failed: "}

// LogLine sorts a deploy log line. Steps are what the agent says ("building",
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

// Step renders a step of the agent: the arrow muted, the text plain, a
// failure in the failed color.
func Step(text string) string {
	if strings.HasPrefix(text, "deploy failed") {
		return Danger.Render(SymbolFailed + " " + text)
	}
	return Muted.Render(SymbolStep) + " " + text
}
