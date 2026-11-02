package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestUninstallKeepsTheApps(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{"agent.json", "server.json", "backup-targets.json", "apps/shop/volumes/data/app.db", "traefik/acme.json"} {
		path := filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	kept, err := Uninstall(context.Background(), dir, false, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var left []string
	for _, entry := range entries {
		left = append(left, entry.Name())
	}
	if !slices.Equal(left, []string{"apps", "traefik"}) {
		t.Errorf("left %v, want apps and traefik", left)
	}
	if len(kept) == 0 || !strings.Contains(kept[0], "shop") {
		t.Errorf("kept %v, want the app shop first", kept)
	}
}
