//go:build docker

package daemon

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepairCatalogEnvFromDocker: when the running release holds no value
// for a catalog secret app.env lacks, the daemon asks Docker for the env
// of the main service's container.
// Run with: go test -tags docker -run RepairCatalogEnvFromDocker ./internal/daemon/
func TestRepairCatalogEnvFromDocker(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir)
	const app = "repair-test"
	if err := os.CopyFS(a.appDir(app), os.DirFS("testdata/beta34/apps/feeds")); err != nil {
		t.Fatal(err)
	}
	// A release from beta.35 names the key, with no value.
	composeJSON := filepath.Join(a.releaseDir(app, 2), "compose.json")
	body, err := os.ReadFile(composeJSON)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.ReplaceAll(string(body), `"ADMIN_PASSWORD": "admin-password-value"`, `"ADMIN_PASSWORD": "${ADMIN_PASSWORD}"`))
	if err := os.WriteFile(composeJSON, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeEnv(a.envFile(app), map[string]string{"VERSION": "2.3.4", "DB_PASSWORD": "db-password-value"}); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), "docker", "run", "-d", "--rm",
		"--label", "com.docker.compose.project="+a.projectOf(app), "--label", "com.docker.compose.service=web",
		"-e", "ADMIN_PASSWORD=from-docker", "alpine:latest", "sleep", "300").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v %s", err, out)
	}
	container := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })

	a.moveOldEnvs(t.Context())
	want := map[string]string{"VERSION": "2.3.4", "DB_PASSWORD": "db-password-value", "ADMIN_PASSWORD": "from-docker"}
	if got, err := a.env(app); err != nil || !maps.Equal(got, want) {
		t.Errorf("env = %v, %v; want %v", got, err, want)
	}
}
