package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What the editor saved is asked about first, then replaces the env vars;
// the file goes either way.
func TestEditedEnvAsksFirst(t *testing.T) {
	write := func(text string) string {
		path := filepath.Join(t.TempDir(), "shop.env")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	current := map[string]string{"KEEP": "1", "GONE": "1"}
	m := press(frames()["detail"], "e")

	path := write("# comment\nKEEP=1\nNEW=2\n")
	next, _ := m.edited(editedMsg{editMsg: editMsg{app: "hello", path: path, env: current}})
	if next.mode != modeConfirm || !next.confirm.danger || !strings.Contains(next.confirm.detail, "adds NEW; removes GONE") {
		t.Errorf("mode %d, confirm %+v", next.mode, next.confirm)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file with the values is still there")
	}

	next, _ = m.edited(editedMsg{editMsg: editMsg{app: "hello", path: write("KEEP=1\nGONE=1\n"), env: current}})
	if next.mode != modeEnv || !strings.Contains(next.notice, "Nothing changed") {
		t.Errorf("the same text: mode %d, notice %q", next.mode, next.notice)
	}
	next, _ = m.edited(editedMsg{editMsg: editMsg{app: "hello", path: write("BROKEN\n"), env: current}})
	if next.mode != modeEnv || next.err == nil || !strings.Contains(next.err.Error(), "line 1") {
		t.Errorf("a broken text: mode %d, err %v", next.mode, next.err)
	}
}
