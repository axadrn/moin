package tui

// The env vars of an app as one .env text in the editor: `e`, then `edit`.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/dotenv"
)

type (
	// editMsg is the .env text of an app, written to path, ready for the
	// editor.
	editMsg struct {
		app, path string
		env       map[string]string
	}
	// editedMsg comes when the editor closed.
	editedMsg struct {
		editMsg
		err error
	}
)

// editEnv writes the env vars of an app to a file only you can read, for
// the editor.
func (m model) editEnv(name string) tea.Cmd {
	return func() tea.Msg {
		env, err := m.api.Env(m.ctx, name)
		if err != nil {
			return errMsg{err}
		}
		file, err := os.CreateTemp("", name+"-*.env")
		if err != nil {
			return errMsg{err}
		}
		text := "# The env vars of " + name + ". Save and close to see what changes; a key you delete is removed.\n" + dotenv.Format(env)
		_, err = file.WriteString(text)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(file.Name())
			return errMsg{err}
		}
		return editMsg{app: name, path: file.Name(), env: env}
	}
}

// editor is the command of $VISUAL or $EDITOR, else vi.
func editor(path string) *exec.Cmd {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(os.Getenv(name)); len(fields) > 0 {
			return exec.Command(fields[0], append(fields[1:], path)...)
		}
	}
	return exec.Command("vi", path)
}

// openEditor hands the terminal to the editor until it closes.
func (m model) openEditor(edit editMsg) tea.Cmd {
	return tea.ExecProcess(editor(edit.path), func(err error) tea.Msg {
		return editedMsg{editMsg: edit, err: err}
	})
}

// edited reads what the editor saved, removes the file and asks before it
// replaces the env vars.
func (m model) edited(msg editedMsg) (model, tea.Cmd) {
	text, err := os.ReadFile(msg.path)
	_ = os.Remove(msg.path)
	if err == nil {
		err = msg.err
	}
	if err != nil {
		m.err = fmt.Errorf("the editor: %w; nothing changed", err)
		return m, nil
	}
	next, err := dotenv.Parse(string(text))
	if err != nil {
		m.err = errors.New(err.Error() + "; nothing changed. Type edit to try again")
		return m, nil
	}
	change := dotenv.Diff(msg.env, next)
	if change.None() {
		m.notice = "Nothing changed in the env vars of " + msg.app
		return m, nil
	}
	question := "Replace the env vars of " + msg.app + "?"
	return m.confirmFirst(question, "It "+change.String()+".", "replace", len(change.Removed) > 0, m.replaceEnv(msg.app, next)), nil
}

// replaceEnv sends the env vars and shows them.
func (m model) replaceEnv(name string, env map[string]string) tea.Cmd {
	return func() tea.Msg {
		change, err := m.api.ReplaceEnv(m.ctx, name, env)
		if err != nil {
			return errMsg{err}
		}
		output := m.envOutput(name, env, false)
		output.note = "it " + change.String() + " · the next deploy gets them"
		return output
	}
}
