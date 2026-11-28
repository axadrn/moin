package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestEnvBeforeTheFirstDeploy: an app that needs DATABASE_URL to start
// gets it before its first deploy, which then reads it.
func TestEnvBeforeTheFirstDeploy(t *testing.T) {
	dir := t.TempDir()
	saved, err := SetEnvVarHere(dir, "new-app", "DATABASE_URL", "postgres://db/app")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Live || !slices.Equal(saved.Added, []string{"DATABASE_URL"}) {
		t.Errorf("saved = %+v, want DATABASE_URL added and no live release", saved)
	}
	names, err := EnvNamesHere(dir, "new-app")
	if err != nil || !slices.Equal(names, []string{"DATABASE_URL"}) {
		t.Errorf("names = %v, %v", names, err)
	}
	text, err := EnvFileHere(dir, "new-app")
	if err != nil || !strings.Contains(text, `DATABASE_URL="postgres://db/app"`) {
		t.Errorf("env file = %q, %v", text, err)
	}
	saved, err = SetEnvHere(dir, "new-app", "SECRET_KEY=abc\n")
	if err != nil || !slices.Equal(saved.Added, []string{"SECRET_KEY"}) || !slices.Equal(saved.Removed, []string{"DATABASE_URL"}) {
		t.Errorf("replace = %+v, %v", saved, err)
	}
	if saved, _ := SetEnvVarHere(dir, "new-app", "SECRET_KEY", "abc"); !saved.None() {
		t.Errorf("the same value again = %+v, want no change", saved)
	}
}

// TestEnvVarSetKeepsTheOthers: setting one env var leaves the others, and
// a value with a line break comes back the same.
func TestEnvVarSetKeepsTheOthers(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir)
	os.MkdirAll(a.appDir("shop"), 0o700)
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("2"), 0o600)
	if _, err := SetEnvHere(dir, "shop", "PORT=3000\n"); err != nil {
		t.Fatal(err)
	}
	key := "-----BEGIN KEY-----\nabc\n-----END KEY-----"
	saved, err := SetEnvVarHere(dir, "shop", "PRIVATE_KEY", key)
	if err != nil || !saved.Live {
		t.Fatalf("saved = %+v, %v: want the live release to start with it", saved, err)
	}
	env, _ := a.env("shop")
	if env["PORT"] != "3000" || env["PRIVATE_KEY"] != key {
		t.Errorf("env = %q", env)
	}
}

// TestEnvRefusesBadNames: no app name with a slash, no env name with a
// dash.
func TestEnvRefusesBadNames(t *testing.T) {
	dir := t.TempDir()
	if _, err := SetEnvVarHere(dir, "../etc", "A", "b"); err == nil {
		t.Error("app ../etc: want an error")
	}
	if _, err := SetEnvVarHere(dir, "shop", "MY-KEY", "b"); err == nil {
		t.Error("env name MY-KEY: want an error")
	}
	if _, err := EnvNamesHere(dir, "Shop"); err == nil {
		t.Error("app Shop: want an error")
	}
}

// TestComposeAndRemoveNeedARelease: an app no deploy made has no compose
// file and nothing to remove; the CLI then looks on other servers.
func TestComposeAndRemoveNeedARelease(t *testing.T) {
	dir := t.TempDir()
	if _, err := ComposeHere(dir, "new-app"); !errors.Is(err, ErrNoApp) {
		t.Errorf("compose = %v, want ErrNoApp", err)
	}
	if err := RemoveHere(t.Context(), dir, "new-app"); !errors.Is(err, ErrNoApp) {
		t.Errorf("rm = %v, want ErrNoApp", err)
	}
}
