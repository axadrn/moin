//go:build docker

package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// TestRenameDocker follows one app from before renames through the
// update and a rename, with one set of containers: its volume, named by
// compose after the old project, keeps its data and its name; the old
// containers go when the new ones start; after the rename the containers
// carry the new name, the data is still there and nothing of the old
// name runs.
func TestRenameDocker(t *testing.T) {
	ctx := context.Background()
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	folder, renamed := "rntest-"+hex.EncodeToString(suffix), "rntest-renamed-"+hex.EncodeToString(suffix)
	appID := "0f8e2c4a-1b3d-4e5f-8a9b-" + hex.EncodeToString(suffix) + "000000"
	legacyVolume := project(folder) + "_data"
	defer func(stable, window time.Duration) { stableFor, startWindow = stable, window }(stableFor, startWindow)
	stableFor, startWindow = time.Second, 20*time.Second

	compose := "services:\n  web:\n    image: alpine:latest\n    command: [\"sh\", \"-c\", \"sleep 3600\"]\n    volumes:\n      - data:/data\nvolumes:\n  data: {}\n"
	source := tarball(t, map[string]string{"compose.yml": compose})
	sum := sha256.Sum256(source)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(source) }))
	defer server.Close()

	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() {
		for _, name := range []string{project(folder), folder, renamed} {
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
	var done []protocol.DeployDone
	a := newApps(t.TempDir(), func(messageType protocol.MessageType, _ string, data any) {
		mu.Lock()
		defer mu.Unlock()
		switch value := data.(type) {
		case protocol.DeployLog:
			lines = append(lines, value.Line)
		case protocol.DeployDone:
			done = append(done, value)
		}
	})
	deploy := func(start protocol.DeployStart) {
		t.Helper()
		a.deploy(ctx, start)
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
	deploy(protocol.DeployStart{DeployID: "d1", App: folder, Name: folder, AppID: appID, Number: 1, SourceURL: server.URL, SHA256: hex.EncodeToString(sum[:])})
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

	// The rename: a restart under the new name.
	deploy(protocol.DeployStart{DeployID: "d2", App: folder, Name: renamed, AppID: appID, Number: 2})
	if got := running(folder); len(got) != 0 {
		t.Fatalf("containers of the old name are left: %v", got)
	}
	if got := running(renamed); len(got) != 1 || got[0] != renamed+"-web-1" {
		t.Fatalf("containers = %v, want %s-web-1", got, renamed)
	}
	if got := read(renamed + "-web-1"); got != "before\nafter" {
		t.Fatalf("data after the rename = %q", got)
	}
	if got := docker("inspect", "-f", `{{range .Mounts}}{{.Name}}{{end}}`, renamed+"-web-1"); got != legacyVolume {
		t.Fatalf("mounted volume = %s, want %s", got, legacyVolume)
	}
	if got := docker("inspect", "-f", `{{index .Config.Labels "moin.app"}}`, renamed+"-web-1"); got != folder {
		t.Fatalf("app label = %q, want the folder", got)
	}
	if got := a.projectOf(folder); got != renamed {
		t.Fatalf("project = %s", got)
	}

	// Logs, stop and a rollback to v1 all reach the renamed containers.
	logs, err := a.logs(ctx, protocol.LogsRequest{App: folder, Tail: 5})
	if err != nil {
		t.Fatal(err)
	}
	_ = logs
	deploy(protocol.DeployStart{DeployID: "d3", App: folder, Name: renamed, AppID: appID, Number: 3, FromNumber: 1})
	if got := running(renamed); len(got) != 1 || got[0] != renamed+"-web-1" {
		t.Fatalf("containers after the rollback = %v", got)
	}
	if got := read(renamed + "-web-1"); got != "before\nafter" {
		t.Fatalf("data after the rollback = %q", got)
	}
	if err := a.stop(ctx, folder); err != nil {
		t.Fatal(err)
	}
	if got := docker("inspect", "-f", "{{.State.Running}}", renamed+"-web-1"); got != "false" {
		t.Fatalf("running after stop = %s", got)
	}
}
