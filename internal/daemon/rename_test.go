package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestPinNames: the volumes compose named after the project get their
// fixed names and the networks follow the project, while names someone
// chose and external ones stay; every service carries its app.
func TestPinNames(t *testing.T) {
	config := map[string]any{
		"services": map[string]any{
			"web": map[string]any{"image": "web", "labels": map[string]any{"keep": "me"}},
			"db":  map[string]any{"image": "postgres"},
		},
		"volumes": map[string]any{
			"data":   map[string]any{"name": "moin-blog_data"},
			"mine":   map[string]any{"name": "chosen"},
			"shared": map[string]any{"name": "shared", "external": true},
			"bare":   nil,
		},
		"networks": map[string]any{
			"default":    map[string]any{"name": "moin-blog_default"},
			"back":       map[string]any{"name": "journal_back"},
			"chosen":     map[string]any{"name": "lan"},
			proxyNetwork: map[string]any{"name": proxyNetwork, "external": true},
		},
	}
	pinNames(config, "blog", "diary", func(volume string) string { return "fixed-" + volume })
	volumes := config["volumes"].(map[string]any)
	for key, want := range map[string]string{"data": "fixed-data", "mine": "chosen", "shared": "shared", "bare": "fixed-bare"} {
		if got := volumes[key].(map[string]any)["name"]; got != want {
			t.Errorf("volume %s = %v, want %s", key, got, want)
		}
	}
	networks := config["networks"].(map[string]any)
	for key, want := range map[string]string{"default": "diary_default", "back": "diary_back", "chosen": "lan", proxyNetwork: proxyNetwork} {
		if got := networks[key].(map[string]any)["name"]; got != want {
			t.Errorf("network %s = %v, want %s", key, got, want)
		}
	}
	for name, item := range config["services"].(map[string]any) {
		labels := item.(map[string]any)["labels"].(map[string]any)
		if labels[labelApp] != "blog" {
			t.Errorf("service %s carries app %v", name, labels[labelApp])
		}
	}
	if config["services"].(map[string]any)["web"].(map[string]any)["labels"].(map[string]any)["keep"] != "me" {
		t.Error("the service lost a label of its own")
	}
}

// TestVolumeNames: a new app's volume is named after its ID, once, and
// keeps that name; an app from before renames keeps the name compose
// gave it (TestRenameDocker).
func TestVolumeNames(t *testing.T) {
	a := newApps(t.TempDir(), func(protocol.MessageType, string, any) {})
	app, id := "volume-test-"+strings.ToLower(filepath.Base(t.TempDir())), "0f8e2c4a-1b3d-4e5f-8a9b-0c1d2e3f4a5b"
	if err := os.MkdirAll(a.appDir(app), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got := a.volumeName(ctx, app, id, "data"); got != "moin-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b-data" {
		t.Fatalf("volume = %s", got)
	}
	if got := a.volumeName(ctx, app, "", "data"); got != "moin-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b-data" {
		t.Fatalf("the second time volume = %s, want the one kept", got)
	}
	if got := a.volumeNames(app); len(got) != 1 || got[0] != "moin-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b-data" {
		t.Fatalf("volume names = %v", got)
	}
}

// TestProjectOf: an app runs as the project its file names, an app from
// before renames as moin-<folder>; a deploy names it by its name now.
func TestProjectOf(t *testing.T) {
	a := newApps(t.TempDir(), func(protocol.MessageType, string, any) {})
	if got := a.projectOf("blog"); got != "moin-blog" {
		t.Fatalf("project = %s", got)
	}
	if err := os.MkdirAll(a.appDir("blog"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.appDir("blog"), "project"), []byte("journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.projectOf("blog"); got != "journal" {
		t.Fatalf("project = %s", got)
	}
	if got := nameOf(protocol.DeployStart{App: "blog", Name: "diary"}); got != "diary" {
		t.Fatalf("name = %s", got)
	}
	if got := nameOf(protocol.DeployStart{App: "blog"}); got != "blog" {
		t.Fatalf("name from an older hub = %s", got)
	}
	// A project another app runs as is refused.
	if err := os.MkdirAll(a.appDir("notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.switchProject(context.Background(), "notes", "journal", func(string, ...any) {}); err == nil {
		t.Fatal("notes took the project of blog")
	}
}
