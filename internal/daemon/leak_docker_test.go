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
// see the secret, and the proxy asks for the app's one password, through a
// restart with a new password and rollbacks with it on and off. Yet no file of the daemon but app.env
// and the htpasswd files holds the secret or a password hash, and neither
// does any message the daemon sends, the compose file of each deploy too.
// A release from before beta.35, with the value in its base.json, keeps
// its compose file on the server.
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
	defer func(old string) { githubAPI = old }(githubAPI)
	githubAPI = server.URL
	defer server.Close()

	var mu sync.Mutex
	var sent [][]byte
	var done []protocol.DeployDone
	dir := t.TempDir()
	a := newApps(dir)
	send := func(data any) {
		body, _ := json.Marshal(data)
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, body)
		if value, ok := data.(protocol.DeployDone); ok {
			done = append(done, value)
		}
	}
	a.proxyMode = func() string { return "tunnel" }
	if _, err := a.setEnv(protocol.EnvSet{App: app, Set: map[string]string{"DB_PASSWORD": secret}}); err != nil {
		t.Fatal(err)
	}
	hashes := []string{}
	withPassword := func(password string) *protocol.Domains {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, string(hash))
		return &protocol.Domains{Domains: []string{app + ".example.net"}, Password: "ada:" + string(hash)}
	}
	deploy := func(start protocol.DeployStart) {
		t.Helper()
		start.App, start.Name, start.DeployID = app, app, "d"+start.DeployID
		deployTo(ctx, a, start, send)
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
		if t.Failed() {
			body, _ := os.ReadFile(a.composeJSON(app))
			version, _ := exec.Command("docker", "compose", "version").CombinedOutput()
			t.Fatalf("%s%s", version, body)
		}
	}

	deploy(protocol.DeployStart{DeployID: "1", Number: 1, SourceURL: server.URL + "/repos/me/leak/tarball/abc123", Domains: withPassword("first-password")})
	seesValues(secret)
	asks(t, app, "first-password")
	// A restart with a new value and a new password: Docker fills in the
	// new value, and the proxy asks for the new password.
	if _, err := a.setEnv(protocol.EnvSet{App: app, Set: map[string]string{"DB_PASSWORD": changed}}); err != nil {
		t.Fatal(err)
	}
	deploy(protocol.DeployStart{DeployID: "2", Number: 2, Domains: withPassword("second-password")})
	seesValues(changed)
	asks(t, app, "second-password")
	// A rollback to v1 runs v1 again, with the env and the password as they
	// are now: an app has one password.
	deploy(protocol.DeployStart{DeployID: "3", Number: 3, FromNumber: 1, Domains: withPassword("third-password")})
	seesValues(changed)
	asks(t, app, "third-password")
	// With the password off, a rollback asks for none.
	deploy(protocol.DeployStart{DeployID: "4", Number: 4, FromNumber: 2, Domains: &protocol.Domains{Domains: []string{app + ".example.net"}}})
	seesValues(changed)
	asks(t, app, "")

	// compose writes a $ of a value as $$, so a hash in a compose file reads
	// $$2a$$04$$...: what follows its last $, the salt and the digest, is
	// searched too.
	leaks := []string{secret, changed}
	for _, hash := range hashes {
		leaks = append(leaks, hash, hash[strings.LastIndex(hash, "$")+1:])
	}
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

	// A deploy from before beta.35 wrote the values into base.json, and a
	// restart takes its compose file from there. That compose file stays
	// on the server; a deploy from source writes ${DB_PASSWORD} again.
	base := filepath.Join(a.releaseDir(app, 4), "base.json")
	body, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, bytes.ReplaceAll(body, []byte("${DB_PASSWORD}"), []byte(changed)), 0o600); err != nil {
		t.Fatal(err)
	}
	deploy(protocol.DeployStart{DeployID: "5", Number: 5})
	seesValues(changed)
	deploy(protocol.DeployStart{DeployID: "6", Number: 6, SourceURL: server.URL + "/repos/me/leak/tarball/abc123"})
	seesValues(changed)

	mu.Lock()
	defer mu.Unlock()
	if baked := done[4]; baked.ComposeFile != "" {
		t.Errorf("the restart of a baked base.json sent its compose file:\n%s", baked.ComposeFile)
	}
	if !bytes.Contains(bytes.Join(sent, nil), []byte("it holds the value of DB_PASSWORD, so it stays on the server")) {
		t.Error("the deploy log does not say why the compose file stays on the server")
	}
	done = append(done[:4], done[5])
	// Every deploy sends its compose file, with ${DB_PASSWORD} as written.
	// The scan of the messages below covers it, and docker compose reads
	// it as it is, next to a .env, the repo's env file and a password.
	for _, value := range done {
		if !strings.Contains(value.ComposeFile, "${DB_PASSWORD}") {
			t.Errorf("deploy %s sent the compose file:\n%s", value.DeployID, value.ComposeFile)
		}
	}
	elsewhere := t.TempDir()
	for name, body := range map[string]string{"compose.yml": done[2].ComposeFile, ".env": "DB_PASSWORD=x\nREPO_VAR=y\n", "worker.env": "WORKER_PASSWORD=${DB_PASSWORD}\n", "htpasswd": "ada:x\n"} {
		if err := os.WriteFile(filepath.Join(elsewhere, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("docker", "compose", "--project-directory", elsewhere, "-f", filepath.Join(elsewhere, "compose.yml"), "config", "-q").CombinedOutput(); err != nil {
		t.Errorf("docker compose does not read the compose file: %v %s\n%s", err, out, done[2].ComposeFile)
	}
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
// entrypoint, and lets the right one in. Without a password, it lets
// everyone in.
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
	user := "ada"
	if password == "" {
		user = ""
	}
	for status(user, password) != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatalf("the proxy does not let %q in", password)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if password == "" {
		return
	}
	if got := status("", ""); got != http.StatusUnauthorized {
		t.Fatalf("without the password: %d", got)
	}
	if got := status("ada", "wrong"); got != http.StatusUnauthorized {
		t.Fatalf("with a wrong password: %d", got)
	}
}

// TestRemoveWithoutVariable removes an app whose compose file needs a
// variable the env no longer has: remove reads no compose file, and takes
// the containers and networks all the same.
func TestRemoveWithoutVariable(t *testing.T) {
	ctx := context.Background()
	defer func(stable, window time.Duration) { stableFor, startWindow = stable, window }(stableFor, startWindow)
	stableFor, startWindow = time.Second, 30*time.Second
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	app := "gone-" + hex.EncodeToString(suffix)
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", app, "down", "--volumes", "--remove-orphans").Run()
	})
	// Not on the main service: that one reads app.env itself.
	compose := "services:\n  web:\n    image: busybox:latest\n    command: [\"sleep\", \"3600\"]\n  db:\n    image: busybox:latest\n    command: [\"sleep\", \"3600\"]\n    environment:\n      NEED: ${NEED:?set NEED}\n"
	source := tarball(t, map[string]string{"compose.yml": compose})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(source) }))
	defer func(old string) { githubAPI = old }(githubAPI)
	githubAPI = server.URL
	defer server.Close()
	var done protocol.DeployDone
	a := newApps(t.TempDir())
	send := func(data any) {
		if value, ok := data.(protocol.DeployDone); ok {
			done = value
		}
	}
	if _, err := a.setEnv(protocol.EnvSet{App: app, Set: map[string]string{"NEED": "x"}}); err != nil {
		t.Fatal(err)
	}
	deployTo(ctx, a, protocol.DeployStart{DeployID: "d1", App: app, Name: app, Number: 1, SourceURL: server.URL + "/repos/me/gone/tarball/abc123"}, send)
	if !done.OK {
		t.Fatalf("deploy: %s", done.Error)
	}
	if _, err := a.setEnv(protocol.EnvSet{App: app, Remove: []string{"NEED"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.stop(ctx, app); err == nil {
		t.Fatal("stop ran without the variable it needs")
	}
	if err := a.remove(ctx, app); err != nil {
		t.Fatalf("remove: %v", err)
	}
	for _, list := range [][]string{{"ps", "-a"}, {"network", "ls"}} {
		out, _ := exec.Command("docker", append(list, "-q", "--filter", "label=com.docker.compose.project="+app)...).CombinedOutput()
		if strings.TrimSpace(string(out)) != "" {
			t.Errorf("docker %s lists what is left of %s: %s", list[0], app, out)
		}
	}
}
