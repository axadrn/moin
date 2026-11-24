//go:build docker

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// TestRefusedDomainsKeepTheRunningRelease: the daemon reports the domains
// of compose.yml before the release starts. When the hub refuses one,
// the deploy fails before `docker compose up`, and the release that ran
// before keeps running, untouched.
func TestRefusedDomainsKeepTheRunningRelease(t *testing.T) {
	ctx := context.Background()
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	app := "dmtest-" + hex.EncodeToString(suffix)
	defer func(stable, window time.Duration) { stableFor, startWindow = stable, window }(stableFor, startWindow)
	stableFor, startWindow = time.Second, 20*time.Second
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", app, "down", "--volumes", "--remove-orphans").Run()
	})

	// Each release labels its container, so the test sees which one runs.
	// The hub refuses the domains of v2; which they are does not matter
	// here, and without domains no proxy starts on the test's machine.
	v1 := "services:\n  web:\n    image: alpine:latest\n    command: [\"sh\", \"-c\", \"sleep 3600\"]\n    labels:\n      release: \"1\"\n"
	v2 := strings.Replace(v1, `release: "1"`, `release: "2"`, 1)
	var source []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(source) }))
	defer server.Close()

	var mu sync.Mutex
	var lines []string
	var done []protocol.DeployDone
	var asked []protocol.DeployDomains
	a := newApps(t.TempDir(), func(_ protocol.MessageType, _ string, data any) {
		mu.Lock()
		defer mu.Unlock()
		switch value := data.(type) {
		case protocol.DeployLog:
			lines = append(lines, value.Line)
		case protocol.DeployDone:
			done = append(done, value)
		}
	})
	a.ask = func(_ context.Context, _ protocol.MessageType, data any) error {
		domains := data.(protocol.DeployDomains)
		asked = append(asked, domains)
		if domains.DeployID == "d2" {
			return errors.New("shop.example.net is already used by another app or service")
		}
		return nil
	}

	source = tarball(t, map[string]string{"compose.yml": v1})
	a.deploy(ctx, protocol.DeployStart{DeployID: "d1", App: app, Name: app, Number: 1, SourceURL: server.URL + "/repos/me/shop/tarball/v1"}, nil)
	source = tarball(t, map[string]string{"compose.yml": v2})
	a.deploy(ctx, protocol.DeployStart{DeployID: "d2", App: app, Name: app, Number: 2, SourceURL: server.URL + "/repos/me/shop/tarball/v2"}, nil)

	mu.Lock()
	log := strings.Join(lines, "\n")
	mu.Unlock()
	if len(done) != 2 || !done[0].OK {
		t.Fatalf("v1: %+v\n%s", done, log)
	}
	if done[1].OK || !strings.Contains(done[1].Error, "shop.example.net is already used by another app") {
		t.Fatalf("v2 = %+v\n%s", done[1], log)
	}
	if len(asked) != 2 || asked[1].DeployID != "d2" {
		t.Fatalf("asked %+v", asked)
	}
	// Only v1 got as far as starting.
	starts := 0
	for _, line := range lines {
		if line == "starting" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("v2 started:\n%s", log)
	}
	out, err := exec.CommandContext(ctx, "docker", "ps", "--filter", "label=com.docker.compose.project="+app, "--format", `{{.Label "release"}}`).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Fatalf("running releases %q %v\n%s", out, err, log)
	}
	if a.current(app) != 1 {
		t.Fatalf("current = %d", a.current(app))
	}
}
