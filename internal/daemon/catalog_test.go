package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCatalogSource: a catalog deploy takes its compose file from the
// catalog built into the daemon, and nothing for a name it does not have.
func TestCatalogSource(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	if err := catalogSource("umami", src); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(src, "compose.yml"))
	if err != nil || !strings.Contains(string(body), "umami") {
		t.Fatalf("compose.yml = %q, %v", body, err)
	}
	for _, name := range []string{"no-such-app", "../etc", "Umami", ""} {
		if err := catalogSource(name, filepath.Join(t.TempDir(), "src")); err == nil {
			t.Errorf("took %q", name)
		}
	}
}
