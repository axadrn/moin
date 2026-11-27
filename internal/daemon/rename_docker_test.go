//go:build docker

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// TestOldProjectDocker follows one app that runs as moin-<folder> through
// a deploy: its volume, named by compose after the old project, keeps its
// data and its name; the old containers go when the new ones start, which
// carry the app's name.
func TestOldProjectDocker(t *testing.T) {
	ctx := context.Background()
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	folder := "rntest-" + hex.EncodeToString(suffix)
	legacyVolume := project(folder) + "_data"
	defer func(stable, window time.Duration) { stableFor, startWindow = stable, window }(stableFor, startWindow)
	stableFor, startWindow = time.Second, 20*time.Second

	compose := "services:\n  web:\n    image: alpine:latest\n    command: [\"sh\", \"-c\", \"sleep 3600\"]\n    volumes:\n      - data:/data\nvolumes:\n  data: {}\n"
	source := sourceFile(t, map[string]string{"compose.yml": compose})

	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() {
		for _, name := range []string{project(folder), folder} {
			_ = exec.Command("docker", "compose", "-p", name, "down", "--volumes", "--remove-orphans").Run()
		}
		_ = exec.Command("docker", "volume", "rm", "-f", legacyVolume).Run()
	})

	// The app as a daemon from before renames ran it: project moin-<folder>,
	// its volume moin-<folder>_data with data in it.
	legacy := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacy, "compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	docker("compose", "-p", project(folder), "-f", filepath.Join(legacy, "compose.yml"), "up", "-d", "--wait")
	docker("compose", "-p", project(folder), "-f", filepath.Join(legacy, "compose.yml"), "exec", "-T", "web", "sh", "-c", "echo before > /data/kept")

	var mu sync.Mutex
	var lines []string
	var done []deployDone
	a := newApps(t.TempDir())
	send := func(data any) {
		mu.Lock()
		defer mu.Unlock()
		switch value := data.(type) {
		case deployLog:
			lines = append(lines, value.Line)
		case deployDone:
			done = append(done, value)
		}
	}
	deploy := func(start protocol.DeployStart) {
		t.Helper()
		deployTo(ctx, a, start, send)
		mu.Lock()
		defer mu.Unlock()
		if last := done[len(done)-1]; !last.OK {
			t.Fatalf("v%d failed: %s\n%s", start.Number, last.Error, strings.Join(lines, "\n"))
		}
	}
	running := func(name string) []string {
		t.Helper()
		return strings.Fields(docker("ps", "-a", "--filter", "label=com.docker.compose.project="+name, "--format", "{{.Names}}"))
	}
	read := func(container string) string {
		t.Helper()
		return docker("exec", container, "cat", "/data/kept")
	}

	// The first deploy after the update keeps the volume and its name and
	// replaces the old containers.
	deploy(protocol.DeployStart{DeployID: "d1", App: folder, Number: 1, SourceFile: source})
	if got := running(project(folder)); len(got) != 0 {
		t.Fatalf("the old containers still exist: %v", got)
	}
	if got := running(folder); len(got) != 1 || got[0] != folder+"-web-1" {
		t.Fatalf("containers = %v, want %s-web-1", got, folder)
	}
	if got := read(folder + "-web-1"); got != "before" {
		t.Fatalf("data after the update = %q", got)
	}
	if got := a.volumeNames(folder); len(got) != 1 || got[0] != legacyVolume {
		t.Fatalf("volumes = %v, want %s kept", got, legacyVolume)
	}
	docker("exec", folder+"-web-1", "sh", "-c", "echo after >> /data/kept")

	if got := docker("inspect", "-f", `{{index .Config.Labels "moin.app"}}`, folder+"-web-1"); got != folder {
		t.Fatalf("app label = %q, want the folder", got)
	}
	if got := a.projectOf(folder); got != folder {
		t.Fatalf("project = %s", got)
	}

	// A rollback to v1 and stop reach the same containers.
	deploy(protocol.DeployStart{DeployID: "d2", App: folder, Number: 2, FromNumber: 1})
	if got := running(folder); len(got) != 1 || got[0] != folder+"-web-1" {
		t.Fatalf("containers after the rollback = %v", got)
	}
	if got := read(folder + "-web-1"); got != "before\nafter" {
		t.Fatalf("data after the rollback = %q", got)
	}
	if err := a.stop(ctx, folder); err != nil {
		t.Fatal(err)
	}
	if got := docker("inspect", "-f", "{{.State.Running}}", folder+"-web-1"); got != "false" {
		t.Fatalf("running after stop = %s", got)
	}
}
