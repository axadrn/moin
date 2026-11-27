package daemon

import (
	"context"
	"os"
	"path/filepath"
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

// TestVolumeNames: a volume is named once, as compose names it for the
// app's first project, and keeps that name.
func TestVolumeNames(t *testing.T) {
	a := newApps(t.TempDir())
	if err := os.MkdirAll(a.appDir("shop"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := a.volumeName("shop", "data"); got != "moin-shop_data" {
		t.Fatalf("volume = %s", got)
	}
	if got := a.volumeNames("shop"); len(got) != 1 || got[0] != "moin-shop_data" {
		t.Fatalf("volume names = %v", got)
	}
}

// TestProjectOf: an app runs as the project its file names, an app from
// before renames as moin-<folder>; a deploy names it by its name now.
func TestProjectOf(t *testing.T) {
	a := newApps(t.TempDir())
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
		t.Fatalf("name of a deploy that names none = %s", got)
	}
	// A project another app runs as is refused.
	if err := os.MkdirAll(a.appDir("notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.switchProject(context.Background(), "notes", "journal", func(string, ...any) {}); err == nil {
		t.Fatal("notes took the project of blog")
	}
}
