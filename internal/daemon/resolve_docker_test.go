//go:build docker

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestResolveKeepsVariables: ${VAR} in compose.yml takes the app's env var,
// so a postgres service gets the password the app keeps in moin, but only
// in what compose fills in: the compose file moin keeps names it.
func TestResolveKeepsVariables(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "services:\n  db:\n    image: postgres:${PG:-17}\n    environment:\n      POSTGRES_PASSWORD: ${DB_PASSWORD}\n"
	if err := os.WriteFile(filepath.Join(src, "compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApps(dir)
	if _, err := a.setEnv(protocol.EnvSet{App: "vars", Set: map[string]string{"DB_PASSWORD": "s3cret'\"$x"}}); err != nil {
		t.Fatal(err)
	}
	config, filled, err := a.readComposeConfig(context.Background(), "vars", src)
	if err != nil {
		t.Fatal(err)
	}
	service := func(config map[string]any) map[string]any {
		return config["services"].(map[string]any)["db"].(map[string]any)
	}
	// compose escapes $ as $$ in what it fills in.
	if got := service(filled)["environment"].(map[string]any)["POSTGRES_PASSWORD"]; got != "s3cret'\"$$x" {
		t.Errorf("filled POSTGRES_PASSWORD = %q", got)
	}
	if got := service(config)["environment"].(map[string]any)["POSTGRES_PASSWORD"]; got != "${DB_PASSWORD}" {
		t.Errorf("kept POSTGRES_PASSWORD = %q", got)
	}
	// The image is a place: it is kept as it is filled in.
	if got := service(config)["image"]; got != "postgres:17" {
		t.Errorf("image = %q", got)
	}
}
