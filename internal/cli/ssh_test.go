package cli

import (
	"os/exec"
	"testing"
)

// TestShellJoin: every word reaches the server as it is, quotes and all.
func TestShellJoin(t *testing.T) {
	words := []string{"psql", "-c", "select 'a'; drop table x", "$HOME", "it's"}
	out, err := exec.Command("sh", "-c", "printf '%s\\n' "+shellJoin(words)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := "psql\n-c\nselect 'a'; drop table x\n$HOME\nit's\n"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// TestGoArch knows the machines moin has builds for.
func TestGoArch(t *testing.T) {
	for machine, want := range map[string]string{"x86_64": "amd64", "aarch64\n": "arm64", "armv7l": ""} {
		if got := goArch(machine); got != want {
			t.Errorf("goArch(%q) = %q, want %q", machine, got, want)
		}
	}
}

// TestReleaseNumber reads v3 and 3, nothing else.
func TestReleaseNumber(t *testing.T) {
	for text, want := range map[string]int{"v3": 3, "12": 12} {
		if got, err := releaseNumber(text); err != nil || got != want {
			t.Errorf("releaseNumber(%q) = %d, %v", text, got, err)
		}
	}
	for _, text := range []string{"v0", "three", ""} {
		if _, err := releaseNumber(text); err == nil {
			t.Errorf("releaseNumber(%q): want an error", text)
		}
	}
}

// TestCatalog lists the apps built into moin, each with its pitch.
func TestCatalog(t *testing.T) {
	entries, err := catalogEntries()
	if err != nil || len(entries) == 0 {
		t.Fatalf("catalog = %d apps, %v", len(entries), err)
	}
	for _, entry := range entries {
		if entry.Title == "" || entry.Pitch == "" {
			t.Errorf("%s has no name or pitch in its app.md", entry.Name)
		}
	}
}
