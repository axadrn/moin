//go:build docker

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestEnvNeverReachesDocker: an app's env fills its compose file, but never
// becomes docker's own environment. Docker reads DOCKER_CONFIG itself, and
// one pointing at the source would run a program from there, as root.
func TestEnvNeverReachesDocker(t *testing.T) {
	a := newApps(t.TempDir())
	src := filepath.Join(a.appDir("web"), "releases", "1", "src")
	plugins := filepath.Join(src, ".docker", "cli-plugins")
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	fake := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(filepath.Join(plugins, "docker-compose"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "services:\n  web:\n    image: nginx:1\n    environment:\n      GREETING: ${GREETING}\n"
	if err := os.WriteFile(filepath.Join(src, "compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"DOCKER_CONFIG": filepath.Join(src, ".docker"), "GREETING": "it's $5\nnow"}
	if _, err := a.setEnv(protocol.EnvSet{App: "web", Set: env}); err != nil {
		t.Fatal(err)
	}
	_, filled, err := a.readComposeConfig(context.Background(), "web", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("docker ran the program the app's env pointed at")
	}
	got := filled["services"].(map[string]any)["web"].(map[string]any)["environment"].(map[string]any)["GREETING"]
	if got != "it's $$5\nnow" {
		t.Fatalf("GREETING = %q", got)
	}
}
