//go:build docker

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// TestRedeployKeepsHowItBuilds: a redeploy builds the source a release
// kept, with the compose.yml moin wrote for it. It writes that file again,
// so a Go repo without a Dockerfile is a Go repo again, with the
// Dockerfile in its compose file, not a compose repo.
func TestRedeployKeepsHowItBuilds(t *testing.T) {
	ctx := context.Background()
	a := newApps(t.TempDir())
	app := "goapp"
	if err := a.ensureEnv(app); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(a.releaseDir(app, 1), "src")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"go.mod": "module goapp\n\ngo 1.26\n", "main.go": "package main\n\nfunc main() {}\n"} {
		if err := os.WriteFile(filepath.Join(first, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, detected, err := a.resolve(ctx, app, first)
	if err != nil || detected.Kind != "go" {
		t.Fatalf("first deploy: %+v, %v", detected, err)
	}
	saveJSON(filepath.Join(a.releaseDir(app, 1), "detected.json"), detected)

	again := &release{start: protocol.DeployStart{App: app, Number: 2, SourceNumber: 1}, dir: a.releaseDir(app, 2), logf: func(string, ...any) {}}
	if err := os.MkdirAll(again.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.fetchSource(ctx, again); err != nil {
		t.Fatal(err)
	}
	build, _ := again.config["services"].(map[string]any)["web"].(map[string]any)["build"].(map[string]any)
	if again.detected.Kind != "go" || !strings.Contains(fmt.Sprint(build["dockerfile_inline"]), "FROM golang:1.26-bookworm") {
		t.Fatalf("redeploy: %+v, build %v", again.detected, build)
	}
}
