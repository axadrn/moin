package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestEnvChangeByValue: moin on the server sees the values, so a key whose
// value stays is no change.
func TestEnvChangeByValue(t *testing.T) {
	change := envChange(map[string]string{"A": "1", "B": "2", "C": "3"}, map[string]string{"A": "1", "B": "two", "D": "4"})
	if !reflect.DeepEqual(change.Added, []string{"D"}) || !reflect.DeepEqual(change.Replaced, []string{"B"}) || !reflect.DeepEqual(change.Removed, []string{"C"}) {
		t.Errorf("change = %+v", change)
	}
	if !envChange(map[string]string{"A": "1"}, map[string]string{"A": "1"}).None() {
		t.Error("the same env is a change")
	}
}

// TestPreviousRelease skips releases with the live images, like a restart,
// and failed ones.
func TestPreviousRelease(t *testing.T) {
	a := hereApps(t.TempDir())
	release := func(number int, kind, image string) {
		dir := a.releaseDir("shop", number)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "kind"), []byte(kind), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "compose.json"), []byte(`{"services": {"web": {"image": "`+image+`"}}}`), 0o600)
	}
	release(1, releaseBuild, "shop:1")
	release(2, releaseBuild, "shop:2")
	release(3, releaseFailed, "shop:3")
	release(4, releaseBuild, "shop:4")
	release(5, releaseApply, "shop:4")
	if got := a.previousRelease("shop", 5); got != 2 {
		t.Errorf("previous of v5 = v%d, want v2: v4 runs the same images, v3 failed", got)
	}
	if got := a.previousRelease("shop", 1); got != 0 {
		t.Errorf("previous of v1 = v%d, want none", got)
	}
	if got := a.releasesText("shop"); got != "v1, v2, v4, v5" {
		t.Errorf("releases = %q", got)
	}
}

// TestKeys reads, adds and refuses keys in authorized_keys.
func TestKeys(t *testing.T) {
	authorizedKeys = filepath.Join(t.TempDir(), "ssh", "authorized_keys")
	ada := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDl4Zpt1qBxP0gT4yQ1yBf1c4aM0f6GmM1tK2f5vH2kQ ada@laptop"
	key, err := AddKeyHere(ada + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if key.Comment != "ada@laptop" || !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Errorf("key = %+v", key)
	}
	if _, err := AddKeyHere(ada); err == nil {
		t.Error("the same key twice: want an error")
	}
	if _, err := AddKeyHere("not a key"); err == nil {
		t.Error("not a key: want an error")
	}
	optioned := `from="203.0.113.7" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPQ1nX3Lf2mZ0bQ4c8yK3sN5rT7uW9xY1zA2bC4dE6fG anna`
	if err := os.WriteFile(authorizedKeys, []byte("# team\n"+ada+"\n"+optioned+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := KeysHere()
	if err != nil || len(keys) != 2 || keys[1].Comment != "anna" {
		t.Fatalf("keys = %+v, %v", keys, err)
	}
	if _, err := RemoveKeyHere(t.Context(), "bob", ""); err == nil {
		t.Error("removing a key that is not there: want an error")
	}
	if _, err := RemoveKeyHere(t.Context(), "anna", ""); err == nil {
		t.Error("without knowing the session's key, moin removes none: want an error")
	}
}
