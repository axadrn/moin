package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// serveRelease serves a release like GitHub does: the binary, SHA256SUMS and
// its signature by key.
func serveRelease(t *testing.T, version string, binary []byte, key ed25519.PrivateKey) *httptest.Server {
	t.Helper()
	asset := build.Name + "-" + runtime.GOOS + "-" + runtime.GOARCH
	sum := sha256.Sum256(binary)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n")
	files := map[string][]byte{
		"/" + version + "/" + asset:       binary,
		"/" + version + "/SHA256SUMS":     sums,
		"/" + version + "/SHA256SUMS.sig": ed25519.Sign(key, sums),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

// trust makes key one of the release keys for the test.
func trust(t *testing.T, key ed25519.PrivateKey) {
	t.Helper()
	before := releaseKeys
	releaseKeys = []string{base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}
	t.Cleanup(func() { releaseKeys = before })
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestUpdateSwapsASignedRelease(t *testing.T) {
	key := newKey(t)
	trust(t, key)
	binary := []byte("#!/bin/sh\necho " + build.Name + " v9.9.9\n")
	server := serveRelease(t, "v9.9.9", binary, key)
	before := releaseURL
	releaseURL = server.URL + "/"
	t.Cleanup(func() { releaseURL = before })

	executable := filepath.Join(t.TempDir(), build.Name)
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := &updater{url: "https://hub.example.com", executable: executable, systemd: build.Name + "-daemon"}
	if err := u.install(context.Background(), protocol.DaemonUpdate{Version: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(executable); string(body) != string(binary) {
		t.Fatalf("binary = %q", body)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(executable), ".*")); len(leftovers) > 0 {
		t.Fatalf("left %v", leftovers)
	}
}

// TestUpdateRefuses: every way an update can be wrong leaves the binary as
// it was. The hub cannot pick the binary, only the version.
func TestUpdateRefuses(t *testing.T) {
	key := newKey(t)
	binary := []byte("#!/bin/sh\necho " + build.Name + " v9.9.9\n")
	for _, test := range []struct {
		name    string
		version string
		running string
		server  *httptest.Server
		trusted ed25519.PrivateKey
		systemd string
		want    string
	}{
		{name: "signed by another key", version: "v9.9.9", server: serveRelease(t, "v9.9.9", binary, newKey(t)), trusted: key, want: "no valid release signature"},
		{name: "binary not the signed one", version: "v9.9.9", server: tampered(t, "v9.9.9", binary, key), trusted: key, want: "does not match its signed SHA-256"},
		{name: "says another version", version: "v9.9.8", server: serveRelease(t, "v9.9.8", binary, key), trusted: key, want: "the new binary says"},
		{name: "older than the running one", version: "v9.9.8", running: "v9.9.9", server: serveRelease(t, "v9.9.8", binary, key), trusted: key, want: "never goes back"},
		{name: "no release", version: "main-b4be272", server: serveRelease(t, "main-b4be272", binary, key), trusted: key, want: "not a release"},
		{name: "not on GitHub", version: "v9.9.7", server: serveRelease(t, "v9.9.9", binary, key), trusted: key, want: "404"},
		{name: "without systemd", version: "v9.9.9", server: serveRelease(t, "v9.9.9", binary, key), trusted: key, systemd: "none", want: "systemd"},
	} {
		t.Run(test.name, func(t *testing.T) {
			trust(t, test.trusted)
			before, version := releaseURL, build.Version
			releaseURL = test.server.URL + "/"
			if test.running != "" {
				build.Version = test.running
			}
			t.Cleanup(func() { releaseURL, build.Version = before, version })

			executable := filepath.Join(t.TempDir(), build.Name)
			if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			u := &updater{url: "https://hub.example.com", executable: executable, systemd: build.Name + "-daemon"}
			if test.systemd == "none" {
				u.systemd = ""
			}
			err := u.install(context.Background(), protocol.DaemonUpdate{Version: test.version})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
			if body, _ := os.ReadFile(executable); string(body) != "old" {
				t.Fatalf("a refused update changed the binary: %q", body)
			}
		})
	}
}

// tampered serves a release whose binary is not the one its signed
// checksums name.
func tampered(t *testing.T, version string, binary []byte, key ed25519.PrivateKey) *httptest.Server {
	t.Helper()
	signed := serveRelease(t, version, binary, key)
	asset := "/" + version + "/" + build.Name + "-" + runtime.GOOS + "-" + runtime.GOARCH
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == asset {
			_, _ = w.Write([]byte("#!/bin/sh\necho evil\n"))
			return
		}
		http.Redirect(w, r, signed.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestReleaseKeys: every release key is a public Ed25519 key, and there
// is a spare.
func TestReleaseKeys(t *testing.T) {
	if len(releaseKeys) < 2 {
		t.Fatalf("%d release keys, want the key in use and a spare", len(releaseKeys))
	}
	for _, encoded := range releaseKeys {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != ed25519.PublicKeySize {
			t.Errorf("release key %q is not a base64 public Ed25519 key", encoded)
		}
	}
}

func TestChecksum(t *testing.T) {
	sums := []byte("aa" + strings.Repeat("0", 62) + "  moin-linux-amd64\n" + "BB" + strings.Repeat("1", 62) + "  moin-linux-arm64\n")
	if got, err := checksum(sums, "moin-linux-arm64"); err != nil || got != "bb"+strings.Repeat("1", 62) {
		t.Fatalf("checksum = %q, %v", got, err)
	}
	if _, err := checksum(sums, "moin-linux-amd"); err == nil {
		t.Fatal("found a checksum for a name that is only a prefix")
	}
}

// TestUpdateWaitsForARestore: a restore counts like a deploy, so an update
// does not stop the daemon in the middle of a swap. Without the wait it
// would reach the updater, which this runner has none of.
func TestUpdateWaitsForARestore(t *testing.T) {
	r := &runner{pending: map[string]protocol.DeployDone{}}
	// A restore runs: an update is refused before it reaches the updater,
	// which this runner has none of.
	if !r.beginWork() {
		t.Fatal("a restore did not start")
	}
	message, _ := protocol.NewMessage(protocol.TypeDaemonUpdate, protocol.DaemonUpdate{Version: "v9"})
	r.daemonUpdate(context.Background(), message, nil)
	if r.updating {
		t.Fatal("an update started while a restore runs")
	}
	// An update runs: no restore or deploy starts until it ends.
	r.endWork()
	if !r.beginUpdate() {
		t.Fatal("an update did not start with nothing running")
	}
	if r.beginWork() {
		t.Fatal("a restore started while the daemon updates itself")
	}
	if r.beginUpdate() {
		t.Fatal("a second update started")
	}
	r.endUpdate()
	if !r.beginWork() {
		t.Fatal("no restore after a failed update")
	}
	r.endWork()

	// A stopping daemon waits for a restore that runs.
	r.beginWork()
	go func() {
		time.Sleep(1500 * time.Millisecond)
		r.endWork()
	}()
	start := time.Now()
	r.waitIdle(time.Minute)
	if waited := time.Since(start); waited < time.Second {
		t.Fatalf("stopped after %s while a restore ran", waited)
	}
}
