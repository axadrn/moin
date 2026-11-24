package tui

// The env vars of an app as one .env text in the editor: `E` on the env
// tab. Values are write-only, so the editor opens without them: the text
// replaces every env var.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/dotenv"
)

type (
	// editMsg is the file for the .env text of an app, ready for the
	// editor, and the keys the app has now.
	editMsg struct {
		app, path string
		keys      []string
	}
	// editedMsg comes when the editor closed.
	editedMsg struct {
		editMsg
		err error
	}
)

// editEnv makes a file only you can read for the editor, which names the
// keys the app has in a comment.
func (m model) editEnv(name string) tea.Cmd {
	api, ctx := m.api, m.ctx
	return func() tea.Msg {
		env, err := api.Env(ctx, name)
		if err != nil {
			return errMsg{err}
		}
		keys := make([]string, len(env))
		for i, v := range env {
			keys[i] = v.Key
		}
		file, err := os.CreateTemp("", name+"-*.env")
		if err != nil {
			return errMsg{err}
		}
		text := "# Every env var of " + name + ", as KEY=value lines. Values are write-only, so none are here.\n" +
			"# Save and close to see what changes. A key that is not in the text is removed.\n"
		if len(keys) > 0 {
			text += "# Now: " + strings.Join(keys, ", ") + "\n"
		}
		_, err = file.WriteString(text)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(file.Name())
			return errMsg{err}
		}
		return editMsg{app: name, path: file.Name(), keys: keys}
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
		m.fail(fmt.Errorf("the editor: %w; nothing changed", err))
		return m, nil
	}
	next, err := dotenv.Parse(string(text))
	if err != nil {
		m.fail(errors.New(err.Error() + "; nothing changed. E tries again"))
		return m, nil
	}
	change := dotenv.Diff(msg.keys, next)
	if change.None() {
		m.say("Nothing changed in the env vars of " + msg.app)
		return m, nil
	}
	question := "Replace the env vars of " + msg.app + "?"
	return m.ask1(question, "It "+change.String()+".", "replace", len(change.Removed) > 0, m.replaceEnv(msg.app, next)), nil
}

// replaceEnv sends the env vars and shows them.
func (m model) replaceEnv(name string, env map[string]string) tea.Cmd {
	api, ctx := m.api, m.ctx
	return func() tea.Msg {
		change, err := api.ReplaceEnv(ctx, name, env)
		if err != nil {
			return errMsg{err}
		}
		return reloadMsg{notice: "Saved: it " + change.String() + ". The next deploy gets them"}
	}
}
