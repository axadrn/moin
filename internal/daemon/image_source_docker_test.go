//go:build docker

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// TestImageWithSource: a deploy that brings a commit and an image, as an
// image deploy of an app that follows a repo does, takes the compose file
// of the commit and runs the image as the main service, without building
// it.
func TestImageWithSource(t *testing.T) {
	ctx := context.Background()
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	app := "imgtest-" + hex.EncodeToString(suffix)
	defer func(stable, window time.Duration) { stableFor, startWindow = stable, window }(stableFor, startWindow)
	stableFor, startWindow = time.Second, 20*time.Second

	// The compose file of the commit builds web; that build would fail.
	compose := "services:\n  web:\n    build: .\n    command: [\"sh\", \"-c\", \"sleep 3600\"]\n    labels:\n      from-commit: \"yes\"\n"
	source := sourceFile(t, map[string]string{"compose.yml": compose, "Dockerfile": "FROM scratch\nRUN false\n"})
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", app, "down", "--volumes", "--remove-orphans").Run()
	})

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
	deployTo(ctx, a, protocol.DeployStart{DeployID: "d1", App: app, Number: 1, SourceFile: source, Image: "alpine:latest"}, send)
	mu.Lock()
	last := done[len(done)-1]
	log := strings.Join(lines, "\n")
	mu.Unlock()
	if !last.OK {
		t.Fatalf("deploy failed: %s\n%s", last.Error, log)
	}
	inspect := func(format string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", format, app+"-web-1").CombinedOutput()
		if err != nil {
			t.Fatalf("docker inspect: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := inspect("{{.Config.Image}}"); got != "alpine:latest" {
		t.Fatalf("web runs %s, want the image of the deploy\n%s", got, log)
	}
	if got := inspect(`{{index .Config.Labels "from-commit"}}`); got != "yes" {
		t.Fatalf("web does not have the compose file of the commit: label %q\n%s", got, log)
	}
	if last.Detected.Image != "alpine:latest" {
		t.Fatalf("detected = %+v", last.Detected)
	}
}
