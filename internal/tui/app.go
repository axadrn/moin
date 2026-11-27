package tui

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/protocol"
)

// keptLines is how many log lines the app screen keeps.
const keptLines = 500

// appScreen is one app opened with enter: its logs, following, and its
// backups.
type appScreen struct {
	key    appKey
	screen int
	// stop ends the logs: it stops ssh and closes the pipe they come in.
	stop  func()
	lines *bufio.Scanner

	logs []string
	// logsEnded says why the logs stopped, empty while they follow.
	logsEnded string

	backups       []protocol.DatabaseBackup
	backupsLoaded bool
	backupsError  string
}

// logLineMsg is one line of an app's logs.
type logLineMsg struct {
	screen int
	line   string
}

// logsEndMsg says the logs of an app stopped.
type logsEndMsg struct {
	screen int
	err    error
}

// backupsMsg is how an app's databases are backed up.
type backupsMsg struct {
	screen    int
	databases []protocol.DatabaseBackup
	err       error
	said      string
}

// openApp shows the app's screen and starts its logs and backups.
func (m *model) openApp(key appKey) tea.Cmd {
	m.screens++
	ctx, cancel := context.WithCancel(m.ctx)
	reader, writer := io.Pipe()
	lines := bufio.NewScanner(reader)
	lines.Buffer(make([]byte, 64*1024), 1024*1024)
	m.app = &appScreen{key: key, screen: m.screens, lines: lines, stop: func() {
		cancel()
		reader.Close()
	}}
	screen, logs, backups := m.screens, m.cli.Logs, m.cli.Backups
	follow := func() tea.Msg {
		go func() {
			writer.CloseWithError(logs(ctx, key.server, key.app, writer))
		}()
		return nextLine(lines, screen)()
	}
	load := func() tea.Msg {
		var said bytes.Buffer
		databases, err := backups(ctx, key.server, key.app, &said)
		return backupsMsg{screen: screen, databases: databases, err: err, said: said.String()}
	}
	return tea.Batch(follow, load)
}

// closeApp goes back to the list and stops the logs.
func (m *model) closeApp() {
	if m.app != nil {
		m.app.stop()
		m.app = nil
	}
}

// nextLine waits for the next line of the logs.
func nextLine(lines *bufio.Scanner, screen int) tea.Cmd {
	return func() tea.Msg {
		if lines.Scan() {
			return logLineMsg{screen: screen, line: lines.Text()}
		}
		return logsEndMsg{screen: screen, err: lines.Err()}
	}
}

// shown is the app screen a message is for, nil when it was closed.
func (m *model) shown(screen int) *appScreen {
	if m.app == nil || m.app.screen != screen {
		return nil
	}
	return m.app
}

func (m *model) gotLogLine(msg logLineMsg) tea.Cmd {
	app := m.shown(msg.screen)
	if app == nil {
		return nil
	}
	app.logs = append(app.logs, cleanLine(msg.line))
	if len(app.logs) > keptLines {
		app.logs = app.logs[len(app.logs)-keptLines:]
	}
	return nextLine(app.lines, app.screen)
}

func (m *model) gotLogsEnd(msg logsEndMsg) {
	app := m.shown(msg.screen)
	if app == nil {
		return
	}
	app.logsEnded = "The logs ended."
	if msg.err != nil {
		app.logsEnded = "The logs stopped: " + m.why(msg.err, strings.Join(app.logs, "\n"))
	}
}

func (m *model) gotBackups(msg backupsMsg) {
	app := m.shown(msg.screen)
	if app == nil {
		return
	}
	app.backups, app.backupsLoaded = msg.databases, true
	if msg.err != nil {
		app.backupsError = m.why(msg.err, msg.said)
	}
}

// why is the reason of a failure: what moin said on the server with ✗,
// else the error in the CLI's words.
func (m *model) why(err error, said string) string {
	for line := range strings.Lines(said) {
		if reason, ok := strings.CutPrefix(strings.TrimSpace(line), "✗ "); ok {
			return reason
		}
	}
	return m.cli.Explain(err)
}

// cleanLine makes a log line safe to draw: no carriage return, tabs as
// spaces.
func cleanLine(line string) string {
	line = strings.TrimRight(line, "\r")
	return strings.ReplaceAll(line, "\t", "    ")
}
