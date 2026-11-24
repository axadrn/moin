package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditedAsksBeforeItReplacesAndRemovesTheFile(t *testing.T) {
	m := fixture(120, 40).withApp("shop", tEnv)
	path := filepath.Join(t.TempDir(), "shop.env")
	if err := os.WriteFile(path, []byte("# only a comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	same, _ := m.edited(editedMsg{editMsg: editMsg{app: "shop", path: path}})
	if same.ask != nil || !strings.Contains(same.notice, "Nothing changed") {
		t.Errorf("no env vars for an app without any asks nothing, got %+v %q", same.ask, same.notice)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the file is removed after the editor")
	}

	if err := os.WriteFile(path, []byte("A=9\nC=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _ := m.edited(editedMsg{editMsg: editMsg{app: "shop", path: path, keys: []string{"A", "B"}}})
	if changed.ask == nil || !changed.ask.danger || !strings.Contains(changed.ask.title, "Replace the env vars of shop") {
		t.Errorf("a change that removes a key asks first, as a danger: %+v", changed.ask)
	}
}
