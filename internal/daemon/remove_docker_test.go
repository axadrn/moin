//go:build docker

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// Removing an app removes the branch it follows and its deploy key, so no
// push brings the app back.
func TestRemoveStopsFollowing(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir)
	os.MkdirAll(a.releaseDir("gone-app", 1), 0o700)
	if err := saveFollow(dir, Follow{App: "gone-app", Repo: "git@example.com:ada/gone-app.git", Branch: "main"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(followDir(dir, "gone-app"), "key"), []byte("key"), 0o600)
	if err := a.remove(t.Context(), "gone-app"); err != nil {
		t.Fatal(err)
	}
	if exists(followDir(dir, "gone-app")) || exists(a.releaseDir("gone-app", 1)) {
		t.Fatal("the follow or the releases stayed")
	}
	if _, err := loadFollow(dir, "gone-app"); err == nil {
		t.Fatal("the app still follows its branch")
	}
}
