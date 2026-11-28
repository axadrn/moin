package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"
)

// terminal wraps stdout and stderr: colors only on a terminal without
// NO_COLOR, plain text for pipes and CI.
func terminal(w io.Writer) io.Writer {
	return colorprofile.NewWriter(w, os.Environ())
}

// raw is w without terminal's writer, which takes out or changes colors a
// terminal cannot show. Bytes from a server, like a copy, the output of
// exec or an app's logs, go out through it as they are.
func raw(w io.Writer) io.Writer {
	if colors, ok := w.(*colorprofile.Writer); ok {
		return colors.Forward
	}
	return w
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
	return sentence(err.Error())
}

// printJSON writes value as indented JSON, for --json.
func printJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// sentence capitalizes the first word, but not one like "v4" or the name
// of moin, which is always lowercase.
func sentence(text string) string {
	runes := []rune(text)
	if len(runes) < 2 || !unicode.IsLower(runes[1]) || strings.HasPrefix(text, build.Name+" ") {
		return text
	}
	return string(unicode.ToUpper(runes[0])) + string(runes[1:])
}

// table writes aligned columns. The header is muted on a terminal; cells may
// carry colors. On a terminal the last column is cut to its width, so a
// row never wraps.
func table(w io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for _, row := range append([][]string{header}, rows...) {
		for column, cell := range row {
			widths[column] = max(widths[column], lipgloss.Width(cell))
		}
	}
	room := 0
	if isTTY(w) {
		room = terminalWidth()
		for _, width := range widths[:len(widths)-1] {
			room -= width + 3
		}
	}
	line := func(row []string) string {
		cells := make([]string, len(row))
		for column, cell := range row {
			if column == len(row)-1 {
				if room > 10 {
					cell = clip(cell, room)
				}
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

// progress prints a log as it streams, like the log of a deploy.
//
// On a terminal every step of the server gets one line: a spinner while
// it runs, then ✓ and how long it took. The tool output of the running
// step, like docker's build lines, shows as its last few lines, dimmed,
// and goes away when the step is done. When it fails, the lines that say
// why stay on the screen.
//
// Without a terminal, as in CI, every line is printed as it comes.
type progress struct {
	w   io.Writer
	tty bool

	mu      sync.Mutex
	current string
	since   time.Time
	tail    []string
	frame   int
	drawn   int
	stop    chan struct{}
	stopped chan struct{}
}

// liveLines is how many tool lines show under the running step; keptLines
// how many stay on the screen when it fails.
const (
	liveLines = 4
	keptLines = 30
)

func newProgress(w io.Writer, current string) *progress {
	p := &progress{w: w, tty: isTTY(w), current: current, since: time.Now(), stop: make(chan struct{}), stopped: make(chan struct{})}
	if !p.tty {
		close(p.stopped)
		return p
	}
	go func() {
		defer close(p.stopped)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				p.mu.Lock()
				p.frame++
				p.clear()
				p.draw()
				p.mu.Unlock()
			}
		}
	}()
	return p
}

// line takes one line of the log.
func (p *progress) line(raw string) {
	text, isStep := style.LogLine(raw)
	if text == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tty {
		if isStep {
			fmt.Fprintln(p.w, style.Step(text))
		} else {
			fmt.Fprintln(p.w, "    "+style.Noise(text))
		}
		return
	}
	p.clear()
	switch {
	case !isStep:
		p.tail = append(p.tail, text)
		if len(p.tail) > keptLines {
			p.tail = p.tail[len(p.tail)-keptLines:]
		}
	case strings.HasSuffix(text, " is live"):
		// The result says it, with the address.
		p.finish(false)
	case strings.HasPrefix(text, "deploy failed"):
		p.finish(true)
		fmt.Fprintln(p.w, style.Step(text))
	default:
		p.finish(false)
		p.current, p.since = text, time.Now()
	}
	p.draw()
}

// finish writes the running step as done, or as failed with the lines
// that say why; the caller holds mu.
func (p *progress) finish(failed bool) {
	if p.current == "" {
		return
	}
	took := style.Muted.Render("  " + human.Duration(time.Since(p.since)))
	if failed {
		for _, line := range p.tail {
			fmt.Fprintln(p.w, "    "+style.Noise(line))
		}
		fmt.Fprintln(p.w, style.Danger.Render(style.SymbolFailed)+" "+p.current+took)
	} else {
		fmt.Fprintln(p.w, style.Success.Render(style.SymbolDone)+" "+p.current+took)
	}
	p.current, p.tail = "", nil
}

// draw writes the spinner with the running step and its last tool lines;
// the caller holds mu.
func (p *progress) draw() {
	if !p.tty || p.current == "" {
		return
	}
	// One row each: a wrapped line would not clear.
	width := terminalWidth()
	frame := style.Spinner[p.frame%len(style.Spinner)]
	took := ""
	if since := time.Since(p.since); since >= time.Second {
		took = "  " + human.Duration(since)
	}
	lines := []string{style.Warning.Render(frame) + " " + clip(p.current, max(width-2-len(took), 10)) + style.Muted.Render(took)}
	for _, line := range p.tail[max(len(p.tail)-liveLines, 0):] {
		lines = append(lines, "    "+style.Dim.Render(clip(line, max(width-5, 10))))
	}
	fmt.Fprint(p.w, strings.Join(lines, "\n"))
	p.drawn = len(lines)
}

// clear removes what draw wrote; the caller holds mu.
func (p *progress) clear() {
	if p.drawn == 0 {
		return
	}
	fmt.Fprint(p.w, "\r\x1b[2K"+strings.Repeat("\x1b[1A\x1b[2K", p.drawn-1))
	p.drawn = 0
}

// end stops the spinner and writes the last step as done, or as failed.
func (p *progress) end(failed bool) {
	if p.tty {
		close(p.stop)
	}
	<-p.stopped
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
	if p.tty {
		p.finish(failed)
	}
}

// terminalWidth is how many columns the terminal has, 80 when it does
// not say.
func terminalWidth() int {
	if columns, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && columns > 0 {
		return columns
	}
	return 80
}

// clip cuts text to width cells and ends it with … when it was longer.
func clip(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(text) + "…"
}
