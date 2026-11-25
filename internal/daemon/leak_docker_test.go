//go:build docker

package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/axadrn/moin/protocol"
)

// TestNoEnvValueLeaks deploys an app that uses a secret of its env in
// every way a compose file can: in environment, inside a URL, through an
// env_file of its source, and with the app's password on. The containers
// see the secret, and the proxy asks for the password, through a restart
// with a new password and a rollback. Yet no file of the daemon but app.env
// and the htpasswd files holds the secret or a password hash, and neither
// does any message the daemon sends.
func TestNoEnvValueLeaks(t *testing.T) {
	if exec.Command("docker", "inspect", proxyName).Run() == nil {
		t.Skipf("a %s runs on this machine: the test would replace it", proxyName)
	}
	ctx := context.Background()
	defer func(stable, window time.Duration) { stableFor, startWindow = stable, window }(stableFor, startWindow)
	stableFor, startWindow = time.Second, 30*time.Second
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	app := "leak-" + hex.EncodeToString(suffix)
	secret := "s3cret-" + hex.EncodeToString(suffix) + "-value"
	changed := "changed-" + hex.EncodeToString(suffix) + "-value"
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", app, "down", "--volumes", "--remove-orphans").Run()
		_ = exec.Command("docker", "rm", "-f", proxyName).Run()
		_ = exec.Command("docker", "network", "rm", proxyNetwork).Run()
	})

	compose := `services:
  web:
    image: busybox:latest
    command: ["sh", "-c", "mkdir -p /www && echo ok > /www/index.html && httpd -f -p 8080 -h /www"]
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      DATABASE_URL: postgres://u:${DB_PASSWORD}@db/x
      FROM_REPO: ${REPO_VAR}
  worker:
    image: busybox:latest
    command: ["sleep", "3600"]
    env_file: [worker.env]
`
	source := tarball(t, map[string]string{
		"compose.yml": compose,
		"worker.env":  "WORKER_PASSWORD=${DB_PASSWORD}\n",
		".env":        "REPO_VAR=from-the-repo\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(source) }))
	defer server.Close()

	var mu sync.Mutex
	var sent [][]byte
	var done []protocol.DeployDone
	dir := t.TempDir()
	a := newApps(dir, func(_ protocol.MessageType, _ string, data any) {
		body, _ := json.Marshal(data)
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, body)
		if value, ok := data.(protocol.DeployDone); ok {
			done = append(done, value)
		}
	})
	a.proxyMode = func() string { return "tunnel" }
	if _, err := a.setEnv(protocol.EnvSet{App: app, Set: map[string]string{"DB_PASSWORD": secret}}); err != nil {
		t.Fatal(err)
	}
	hashes := []string{}
	protect := func(password string) *protocol.Domains {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, string(hash))
		return &protocol.Domains{Domains: []string{app + ".example.net"}, Protect: "ada:" + string(hash)}
	}
	deploy := func(start protocol.DeployStart) {
		t.Helper()
		start.App, start.Name, start.DeployID = app, app, "d"+start.DeployID
		a.deploy(ctx, start, nil)
		mu.Lock()
		defer mu.Unlock()
		if last := done[len(done)-1]; !last.OK {
			t.Fatalf("v%d failed: %s", start.Number, last.Error)
		}
	}
	env := func(service string) string {
		t.Helper()
		out, err := exec.Command("docker", "exec", app+"-"+service+"-1", "env").CombinedOutput()
		if err != nil {
			t.Fatalf("env of %s: %v %s", service, err, out)
		}
		return string(out)
	}
	// The containers see the values: the app's env, the source's .env and
	// the env_file of the source, filled in by Docker.
	seesValues := func(secret string) {
		t.Helper()
		web, worker := env("web"), env("worker")
		for _, line := range []string{"POSTGRES_PASSWORD=" + secret, "DB_PASSWORD=" + secret, "DATABASE_URL=postgres://u:" + secret + "@db/x", "FROM_REPO=from-the-repo"} {
			if !strings.Contains(web, line+"\n") {
				t.Errorf("web lacks %s:\n%s", line, web)
			}
		}
		if !strings.Contains(worker, "WORKER_PASSWORD="+secret+"\n") {
			t.Errorf("worker lacks its env_file:\n%s", worker)
		}
	}

	deploy(protocol.DeployStart{DeployID: "1", Number: 1, SourceURL: server.URL + "/repos/me/leak/tarball/abc123", Domains: protect("first-password")})
	seesValues(secret)
	asks(t, app, "first-password")
	// A restart with a new value and a new password: Docker fills in the
	// new value, and the proxy asks for the new password.
	if _, err := a.setEnv(protocol.EnvSet{App: app, Set: map[string]string{"DB_PASSWORD": changed}}); err != nil {
		t.Fatal(err)
	}
	deploy(protocol.DeployStart{DeployID: "2", Number: 2, Domains: protect("second-password")})
	seesValues(changed)
	asks(t, app, "second-password")
	// A rollback to v1 runs v1 again, with its password and the env as it
	// is now.
	deploy(protocol.DeployStart{DeployID: "3", Number: 3, FromNumber: 1, Domains: protect("third-password")})
	seesValues(changed)
	asks(t, app, "first-password")

	leaks := append([]string{secret, changed}, hashes...)
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() == "app.env" || entry.Name() == "htpasswd" {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, leak := range leaks {
			if bytes.Contains(body, []byte(leak)) {
				t.Errorf("%s holds %q", path, leak)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, body := range sent {
		for _, leak := range leaks {
			// JSON writes $ as it is; a hash has no characters it escapes.
			if bytes.Contains(body, []byte(leak)) {
				t.Errorf("a message holds %q: %s", leak, body)
			}
		}
	}
}

// asks checks that the proxy asks for the app's password on its tunnel
// entrypoint, and lets the right one in.
func asks(t *testing.T, app, password string) {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "-f", "{{(index .NetworkSettings.Networks \""+proxyNetwork+"\").IPAddress}}", proxyName).Output()
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + strings.TrimSpace(string(out)) + ":" + tunnelPort + "/"
	status := func(user, password string) int {
		request, _ := http.NewRequest(http.MethodGet, url, nil)
		request.Host = app + ".example.net"
		if user != "" {
			request.SetBasicAuth(user, password)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return 0
		}
		response.Body.Close()
		return response.StatusCode
	}
	// The proxy takes a moment to see the new containers.
	deadline := time.Now().Add(30 * time.Second)
	for status("ada", password) != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatalf("the proxy does not let %q in", password)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if got := status("", ""); got != http.StatusUnauthorized {
		t.Fatalf("without the password: %d", got)
	}
	if got := status("ada", "wrong"); got != http.StatusUnauthorized {
		t.Fatalf("with a wrong password: %d", got)
	}
}
