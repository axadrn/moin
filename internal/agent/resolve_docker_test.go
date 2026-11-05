//go:build docker

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestResolveFillsVariables: ${VAR} in compose.yml takes the app's env var,
// so a postgres service gets the password the app keeps in moin.
func TestResolveFillsVariables(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "services:\n  db:\n    image: postgres:17\n    environment:\n      POSTGRES_PASSWORD: ${DB_PASSWORD}\n"
	if err := os.WriteFile(filepath.Join(src, "compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApps(dir, func(protocol.MessageType, string, any) {})
	config, _, err := a.resolve(context.Background(), "vars", src, map[string]string{"DB_PASSWORD": "s3cret'\"$x"})
	if err != nil {
		t.Fatal(err)
	}
	services := config["services"].(map[string]any)
	env := services["db"].(map[string]any)["environment"].(map[string]any)
	// compose escapes $ as $$ in what it resolves; compose up reads it back.
	if got := env["POSTGRES_PASSWORD"]; got != "s3cret'\"$$x" {
		t.Errorf("POSTGRES_PASSWORD = %q", got)
	}
}
