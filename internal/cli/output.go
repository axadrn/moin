package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"
)

// terminal wraps stdout and stderr: colors only on a terminal without
// NO_COLOR, plain text for pipes and CI.
func terminal(w io.Writer) io.Writer {
	return colorprofile.NewWriter(w, os.Environ())
}

// isTTY reports whether w is a terminal, so spinners and symbols make sense.
func isTTY(w io.Writer) bool {
	writer, ok := w.(*colorprofile.Writer)
	return ok && writer.Profile > colorprofile.NoTTY
}

// hasColor reports whether w shows colors.
func hasColor(w io.Writer) bool {
	writer, ok := w.(*colorprofile.Writer)
	return ok && writer.Profile > colorprofile.ASCII
}

// command renders a command people type next: in the accent on a color
// terminal, in backticks everywhere else.
func command(w io.Writer, text string) string {
	if hasColor(w) {
		return style.Accent.Render(text)
	}
	return "`" + text + "`"
}

// commands renders every `command` in a message like command does.
func commands(w io.Writer, message string) string {
	parts := strings.Split(message, "`")
	if len(parts)%2 == 0 {
		return message
	}
	var out strings.Builder
	for index, part := range parts {
		if index%2 == 1 {
			part = command(w, part)
		}
		out.WriteString(part)
	}
	return out.String()
}

// done writes a result: ✓ and the text.
func done(w io.Writer, format string, args ...any) {
	fmt.Fprintln(w, style.Success.Render(style.SymbolDone)+" "+commands(w, fmt.Sprintf(format, args...)))
}

// step writes a step: → and the text.
func step(w io.Writer, format string, args ...any) {
	fmt.Fprintln(w, style.Muted.Render(style.SymbolStep)+" "+commands(w, fmt.Sprintf(format, args...)))
}

// hint writes the next thing to do, indented under a result, with the
// command in the accent.
func hint(w io.Writer, lead, next string) {
	fmt.Fprintln(w, "  "+style.Muted.Render(lead)+" "+command(w, next))
}

// errReported is an error whose message is already on the screen.
var errReported = errors.New("reported")

// printError writes an error as ✗ and a sentence that says what to do.
func printError(w io.Writer, err error) {
	message := explain(err)
	fmt.Fprintln(w, style.Danger.Render(style.SymbolFailed)+" "+commands(w, message))
}

// explain turns an error into a sentence: capitalized, with the command to
// run when there is one.
func explain(err error) string {
	var urlErr *url.Error
	var netErr *net.OpError
	var apiErr *client.Error
	switch {
	case errors.Is(err, client.ErrNotLoggedIn):
		return fmt.Sprintf("Not logged in. Run `%s login`, or set OPS_TOKEN in CI", build.Name)
	case errors.As(err, &apiErr) && apiErr.Code == "not_found" && apiErr.Message == "The app was not found." && picked.name != "":
		return fmt.Sprintf("There is no app %s; the name came from %s. Name another with --app, or see yours with `%s apps`", picked.name, picked.from, build.Name)
	case errors.As(err, &apiErr) && apiErr.Code == "insufficient_scope":
		return fmt.Sprintf("This token may only read. Log in with a token that has read and write access: `%s login`", build.Name)
	case errors.As(err, &urlErr) && errors.As(err, &netErr):
		host := urlErr.URL
		if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil {
			host = parsed.Scheme + "://" + parsed.Host
		}
		return fmt.Sprintf("Cannot reach the control plane at %s. Check the connection, or log in to another with `%s login --url <url>`", host, build.Name)
	}
	return sentence(err.Error())
}

// sentence capitalizes the first word, but not one like "v4" or the name
// of ops, which is always lowercase.
func sentence(text string) string {
	runes := []rune(text)
	if len(runes) < 2 || !unicode.IsLower(runes[1]) || strings.HasPrefix(text, build.Name+" ") {
		return text
	}
	return string(unicode.ToUpper(runes[0])) + string(runes[1:])
}

// table writes aligned columns. The header is muted on a terminal; cells may
// carry colors.
func table(w io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for _, row := range append([][]string{header}, rows...) {
		for column, cell := range row {
			widths[column] = max(widths[column], lipgloss.Width(cell))
		}
	}
	line := func(row []string) string {
		cells := make([]string, len(row))
		for column, cell := range row {
			if column == len(row)-1 {
				cells[column] = cell
				continue
			}
			cells[column] = cell + strings.Repeat(" ", widths[column]-lipgloss.Width(cell))
		}
		return strings.Join(cells, "   ")
	}
	fmt.Fprintln(w, style.Muted.Render(line(header)))
	for _, row := range rows {
		fmt.Fprintln(w, line(row))
	}
}

// badge is a colored status with its symbol on a terminal, the plain word in
// pipes, so scripts can read the columns.
func badge(w io.Writer, styled, plain string) string {
	if isTTY(w) {
		return styled
	}
	return plain
}

// progress prints a deploy log as it streams: agent steps with →, tool
// output indented and dimmed. On a terminal a spinner with the current step
// and the time so far stays on the last line.
type progress struct {
	w       io.Writer
	tty     bool
	started time.Time

	mu      sync.Mutex
	current string
	frame   int
	drawn   bool
	stop    chan struct{}
	stopped chan struct{}
}

func newProgress(w io.Writer, current string) *progress {
	p := &progress{w: w, tty: isTTY(w), started: time.Now(), current: current, stop: make(chan struct{}), stopped: make(chan struct{})}
	if !p.tty {
		close(p.stopped)
		return p
	}
	go func() {
		defer close(p.stopped)
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				p.mu.Lock()
				p.frame++
				p.draw()
				p.mu.Unlock()
			}
		}
	}()
	return p
}

// line prints one log line above the spinner.
func (p *progress) line(raw string) {
	text, isStep := style.LogLine(raw)
	if text == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
	if isStep {
		p.current = text
		fmt.Fprintln(p.w, style.Step(text))
	} else {
		fmt.Fprintln(p.w, "    "+style.Noise(text))
	}
	p.draw()
}

// draw writes the spinner line; the caller holds mu.
func (p *progress) draw() {
	if !p.tty {
		return
	}
	frame := style.Spinner[p.frame%len(style.Spinner)]
	elapsed := ""
	if since := time.Since(p.started); since >= time.Second {
		elapsed = " · " + duration(since)
	}
	// One row only: a wrapped spinner line would not clear.
	width := 80
	if columns, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && columns > 0 {
		width = columns
	}
	current := clip(p.current, max(width-3-len(elapsed), 10))
	fmt.Fprint(p.w, "\r\x1b[2K"+style.Warning.Render(frame)+" "+current+style.Muted.Render(elapsed))
	p.drawn = true
}

// clear removes the spinner line; the caller holds mu.
func (p *progress) clear() {
	if p.drawn {
		fmt.Fprint(p.w, "\r\x1b[2K")
		p.drawn = false
	}
}

// end stops the spinner and leaves the log as it is.
func (p *progress) end() {
	if p.tty {
		close(p.stop)
	}
	<-p.stopped
	p.mu.Lock()
	p.clear()
	p.mu.Unlock()
}

func (p *progress) elapsed() string {
	return duration(time.Since(p.started))
}

func duration(d time.Duration) string { return style.Duration(d) }

// clip cuts text to width cells and ends it with … when it was longer.
func clip(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(text) + "…"
}
