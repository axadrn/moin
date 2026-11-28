package tui

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// question is a y/n question before a step that changes a server.
type question struct {
	title string
	text  string
	// yes is what runs on y.
	yes func() tea.Cmd
}

// picker lets you pick one row, like the release a rollback goes back
// to or the copy a restore goes back to.
type picker struct {
	title  string
	rows   []pickerRow
	cursor int
	// picked is what happens with the row you pick.
	picked func(index int)
}

// pickerRow is one row of a picker: what it is, when, and a note.
type pickerRow struct {
	name, when, note string
}

// stepMsg is one line an action's log said, and how to read the next.
type stepMsg struct {
	line string
	next tea.Cmd
}

// doneMsg is how an action went: done says what it did, else err says
// why not.
type doneMsg struct {
	key  appKey
	done string
	err  error
	said string
}

// answer handles a key while a question is open: y runs it, any other
// key changes nothing.
func (m *model) answer(key string) tea.Cmd {
	asked := m.question
	m.question = nil
	if key == "y" || key == "Y" {
		return asked.yes()
	}
	m.message = style.Muted.Render("Nothing changed.")
	return nil
}

// pick handles a key in a picker.
func (m *model) pick(key string) tea.Cmd {
	p := m.picker
	switch key {
	case "up", "k":
		p.cursor = max(p.cursor-1, 0)
	case "down", "j":
		p.cursor = min(p.cursor+1, len(p.rows)-1)
	case "enter":
		m.picker = nil
		p.picked(p.cursor)
	case "esc", "q":
		m.picker = nil
	}
	return nil
}

// ready says whether a new action may start: one runs at a time.
func (m *model) ready() (appKey, bool) {
	if m.action != nil {
		return appKey{}, false
	}
	return m.current()
}

// start runs work in the background. The lines of its log show as its
// steps while it runs, its result after.
func (m *model) start(key appKey, verb, what string, work func(log io.Writer) doneMsg) tea.Cmd {
	m.action = &action{key: key, verb: verb, what: what, since: time.Now()}
	m.message = ""
	reader, writer := io.Pipe()
	lines := bufio.NewScanner(reader)
	lines.Buffer(make([]byte, 64*1024), 1024*1024)
	result := make(chan doneMsg, 1)
	go func() {
		msg := work(writer)
		writer.Close()
		result <- msg
	}()
	var next tea.Cmd
	next = func() tea.Msg {
		if lines.Scan() {
			return stepMsg{line: lines.Text(), next: next}
		}
		// The work ended: drain what it still writes, then its result.
		go io.Copy(io.Discard, reader)
		return <-result
	}
	return next
}

// gotStep shows the step an action is at.
func (m *model) gotStep(msg stepMsg) tea.Cmd {
	if m.action == nil {
		return msg.next
	}
	m.action.said = append(m.action.said, msg.line)
	if text, isStep := style.LogLine(msg.line); isStep {
		m.action.step = text
	}
	return msg.next
}

// gotDone shows how an action went and asks its server again, so the
// lists show what changed.
func (m *model) gotDone(msg doneMsg) tea.Cmd {
	said := msg.said
	if m.action != nil {
		said += strings.Join(m.action.said, "\n")
		took := time.Since(m.action.since)
		m.action = nil
		if msg.err == nil && took >= time.Second {
			msg.done += style.Muted.Render("  " + human.Duration(took))
		}
	}
	if msg.err != nil {
		m.message = failed(m.why(msg.err, said))
		return nil
	}
	m.message = succeeded(msg.done)
	if msg.key.server == "" {
		return nil
	}
	m.forget(msg.key)
	return tea.Batch(m.ask(msg.key.server), m.askFacts())
}

// askRestart asks before the app restarts.
func (m *model) askRestart() tea.Cmd {
	key, ok := m.ready()
	if !ok {
		return nil
	}
	m.question = &question{
		title: "Restart " + key.app,
		text:  fmt.Sprintf("%s on %s stops for a moment and starts again, with its env and domains as they are now.", key.app, key.server),
		yes:   func() tea.Cmd { return m.restart(key) },
	}
	return nil
}

// restart runs the live release of the app again.
func (m *model) restart(key appKey) tea.Cmd {
	restart := m.cli.Restart
	return m.start(key, "restarting", "restarting "+key.app, func(log io.Writer) doneMsg {
		result, err := restart(m.ctx, key.server, key.app, log)
		return doneMsg{key: key, done: fmt.Sprintf("%s runs %s", key.app, releaseName(result.Release)), err: err}
	})
}

// askRollback opens the picker on the releases of the app, the one
// before the live one picked, as the CLI suggests too.
func (m *model) askRollback() tea.Cmd {
	key, ok := m.ready()
	if !ok {
		return nil
	}
	known := m.facts[key]
	if known == nil || !known.loaded {
		m.message = style.Muted.Render("Asking " + key.server + " for the releases of " + key.app + ". Press b again in a moment.")
		return m.askFacts()
	}
	if known.err != "" {
		m.message = failed(known.err)
		return nil
	}
	p := &picker{title: "Go back to which deploy of " + key.app + "?", cursor: -1}
	now := time.Now()
	for index, release := range known.releases {
		note := ""
		switch {
		case release.Live:
			note = style.Success.Render(style.SymbolLive + " live")
		case release.Before:
			note = style.Muted.Render("the one before")
			p.cursor = index
		}
		p.rows = append(p.rows, pickerRow{name: releaseName(release.Number), when: human.Ago(release.At, now), note: note})
	}
	if p.cursor < 0 {
		m.message = failed(key.app + " has no older deploy to go back to")
		return nil
	}
	releases := known.releases
	p.picked = func(index int) {
		number := releases[index].Number
		m.question = &question{
			title: "Roll back " + key.app,
			text:  fmt.Sprintf("Go back to %s of %s? It runs with the env and domains as they are now.", releaseName(number), key.app),
			yes:   func() tea.Cmd { return m.rollback(key, number) },
		}
	}
	m.picker = p
	return nil
}

// rollback runs release number of the app again.
func (m *model) rollback(key appKey, number int) tea.Cmd {
	rollback := m.cli.Rollback
	return m.start(key, "rolling back", "rolling back "+key.app, func(log io.Writer) doneMsg {
		result, err := rollback(m.ctx, key.server, key.app, number, log)
		return doneMsg{key: key, done: fmt.Sprintf("%s runs %s again, as %s", key.app, releaseName(result.From), releaseName(result.Release)), err: err}
	})
}

// askBackup asks before a copy of the app's databases is taken now.
func (m *model) askBackup() tea.Cmd {
	key, ok := m.ready()
	if !ok {
		return nil
	}
	m.question = &question{
		title: "Back up " + key.app,
		text:  fmt.Sprintf("Take a copy of every database of %s now? The app keeps running.", key.app),
		yes:   func() tea.Cmd { return m.backup(key) },
	}
	return nil
}

// backup takes a copy of the app's databases now.
func (m *model) backup(key appKey) tea.Cmd {
	backup := m.cli.Backup
	return m.start(key, "copying", "copying the databases of "+key.app, func(log io.Writer) doneMsg {
		var said bytes.Buffer
		copied, err := backup(m.ctx, key.server, key.app, &said)
		names := []string{}
		for _, copy := range copied {
			names = append(names, copy.Database)
		}
		return doneMsg{key: key, done: "Copied " + strings.Join(names, " and ") + " of " + key.app, err: err, said: said.String()}
	})
}

// askRestore opens the picker on the copies of the app's databases, the
// newest picked.
func (m *model) askRestore() tea.Cmd {
	key, ok := m.ready()
	if !ok {
		return nil
	}
	known := m.facts[key]
	if known == nil || !known.loaded {
		m.message = style.Muted.Render("Asking " + key.server + " for the copies of " + key.app + ". Press enter again in a moment.")
		return m.askFacts()
	}
	type copyOf struct{ database, id string }
	var copies []copyOf
	p := &picker{title: "Put the data of " + key.app + " back to which copy?"}
	now := time.Now()
	for _, database := range known.backups {
		for _, copy := range database.Copies {
			copies = append(copies, copyOf{database.Database, copy.ID})
			p.rows = append(p.rows, pickerRow{name: copy.ID, when: human.Ago(copy.At, now), note: style.Muted.Render(database.Database + "  " + human.Size(float64(copy.Size)))})
		}
		for _, undo := range database.Undos {
			copies = append(copies, copyOf{database.Database, "undo-" + undo.ID})
			p.rows = append(p.rows, pickerRow{name: "undo-" + undo.ID, when: human.Ago(undo.At, now), note: style.Muted.Render(database.Database + "  before a restore")})
		}
	}
	if len(copies) == 0 {
		m.message = failed(key.app + " has no copy to go back to yet. Take one with c")
		return nil
	}
	p.picked = func(index int) {
		chosen := copies[index]
		m.question = &question{
			title: "Restore " + key.app,
			text:  fmt.Sprintf("Put %s of %s back to %s? The app stops for a moment. You can undo it.", chosen.database, key.app, chosen.id),
			yes:   func() tea.Cmd { return m.restore(key, chosen.database, chosen.id) },
		}
	}
	m.picker = p
	return nil
}

// restore puts a database of the app back to a copy.
func (m *model) restore(key appKey, database, to string) tea.Cmd {
	restore := m.cli.Restore
	return m.start(key, "restoring", "restoring "+database+" of "+key.app, func(log io.Writer) doneMsg {
		var said bytes.Buffer
		restored, err := restore(m.ctx, key.server, key.app, database, to, &said)
		return doneMsg{key: key, done: fmt.Sprintf("%s of %s is back at %s. Undo: %s", restored.Database, key.app, restored.To, restored.Undo), err: err, said: said.String()}
	})
}

// openAddress opens the app's first address in the browser.
func (m *model) openAddress() tea.Cmd {
	key, ok := m.current()
	if !ok {
		return nil
	}
	app, _ := m.appStatus(key)
	if len(app.Addresses) == 0 {
		m.message = failed(fmt.Sprintf("%s has no address yet. Add one with `%s domains add %s <domain>`", key.app, build.Name, key.app))
		return nil
	}
	address, open := app.Addresses[0], m.open
	return func() tea.Msg {
		if err := open(address); err != nil {
			return doneMsg{done: "Open it in your browser: " + address}
		}
		return doneMsg{done: "Opened " + address}
	}
}

// composeMsg is the compose file of an app, or why there is none.
type composeMsg struct {
	app  string
	file string
	err  error
}

// askCompose asks the app's server for its compose file.
func (m *model) askCompose() tea.Cmd {
	key, ok := m.current()
	if !ok {
		return nil
	}
	m.message = style.Muted.Render("Asking " + key.server + " for the compose file of " + key.app + ".")
	compose, ctx := m.cli.Compose, m.ctx
	return func() tea.Msg {
		var said bytes.Buffer
		file, err := compose(ctx, key.server, key.app, &said)
		if err != nil {
			err = errors.New(m.why(err, said.String()))
		}
		return composeMsg{app: key.app, file: file, err: err}
	}
}

// gotCompose shows the compose file in $PAGER, else less. The screen
// comes back when you quit it.
func (m *model) gotCompose(msg composeMsg) tea.Cmd {
	if msg.err != nil {
		m.message = failed(msg.err.Error())
		return nil
	}
	m.message = ""
	pager := strings.Fields(os.Getenv("PAGER"))
	if len(pager) == 0 {
		pager = []string{"less"}
	}
	cmd := exec.Command(pager[0], pager[1:]...)
	cmd.Stdin = strings.NewReader(msg.file)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return doneMsg{done: "Save it with " + build.Name + " compose " + msg.app + " > compose.yml", err: err}
	})
}

func succeeded(text string) string {
	return style.Success.Render(style.SymbolDone) + " " + text
}

func failed(text string) string {
	return style.Danger.Render(style.SymbolFailed) + " " + text
}

// releaseName is a release as people write it, like v3.
func releaseName(number int) string {
	return "v" + strconv.Itoa(number)
}
