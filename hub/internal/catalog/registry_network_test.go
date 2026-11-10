//go:build network

package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealRegistries asks the real registries for every catalog image:
// each must list releases. Run: go test -tags network ./hub/internal/catalog/
func TestRealRegistries(t *testing.T) {
	entries, err := Load(os.DirFS(filepath.Join("..", "..", "content")))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	for _, entry := range entries {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		tags, err := registry.Tags(ctx, entry.Image)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", entry.Slug, err)
			continue
		}
		releases := UpToChannel(Releases(tags, entry.Filter), tags, entry.Stable)
		if len(releases) == 0 {
			t.Errorf("%s: no releases among %d tags", entry.Slug, len(tags))
			continue
		}
		date := "no date"
		if !releases[0].Date.IsZero() {
			date = releases[0].Date.Format("2006-01-02")
		}
		t.Logf("%-12s %-36s %3d releases, newest %s (%s)", entry.Slug, entry.Image, len(releases), releases[0].Tag, date)
	}
}
