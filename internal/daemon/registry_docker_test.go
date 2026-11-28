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
	"testing"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/protocol"
)

// startRegistry runs a throwaway registry on this machine and returns its
// address, like localhost:32768. With htpasswd it asks for a login.
func startRegistry(t *testing.T, htpasswd string) string {
	t.Helper()
	args := []string{"run", "-d", "--rm", "-p", "127.0.0.1::5000"}
	if htpasswd != "" {
		file := filepath.Join(t.TempDir(), "htpasswd")
		if err := os.WriteFile(file, []byte(htpasswd), 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-v", file+":/htpasswd:ro", "-e", "REGISTRY_AUTH=htpasswd", "-e", "REGISTRY_AUTH_HTPASSWD_REALM=test", "-e", "REGISTRY_AUTH_HTPASSWD_PATH=/htpasswd")
	}
	id := strings.TrimSpace(testDocker(t, "", append(args, "registry:2")...))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "-v", id).Run() })
	port := strings.TrimSpace(testDocker(t, "", "port", id, "5000/tcp"))
	registry := "localhost" + port[strings.LastIndex(port, ":"):]
	for range 50 {
		if exec.Command("curl", "-s", "-o", os.DevNull, "http://"+registry+"/v2/").Run() == nil {
			return registry
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the registry did not start")
	return ""
}

// testDocker runs docker, with stdin when it has some, and returns what
// it printed.
func testDocker(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	command := exec.Command("docker", args...)
	command.Stdin = strings.NewReader(stdin)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// pushVersion builds a small image that differs by version, pushes it as
// image and removes it here, so the next pull really comes from the
// registry.
func pushVersion(t *testing.T, image, version string) {
	t.Helper()
	testDocker(t, "FROM alpine:latest\nLABEL version="+version+"\n", "build", "-q", "-t", image, "-")
	testDocker(t, "", "push", "-q", image)
	testDocker(t, "", "image", "rm", image)
}

// A server pulls an image from a registry and pins it by its content. An
// app that follows the tag deploys each new image under it once, with the
// compose file of the release that runs.
func TestFollowImageFromARegistry(t *testing.T) {
	ctx := context.Background()
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	app := "regtest-" + hex.EncodeToString(suffix)
	defer func(stable, window time.Duration) { stableFor, startWindow = stable, window }(stableFor, startWindow)
	stableFor, startWindow = time.Second, 20*time.Second

	image := startRegistry(t, "") + "/ada/shop:main"
	pushVersion(t, image, "1")

	// What `moin deploy --image` does on the server: pull, pin, deploy
	// with the compose file of the commit.
	dir := t.TempDir()
	a := newApps(dir)
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", a.projectOf(app), "down", "--volumes", "--remove-orphans").Run()
		_ = exec.Command("sh", "-c", "docker image rm "+image+" $(docker image ls -q --filter reference="+project(app)+")").Run()
	})
	pulled, err := PullHere(ctx, image)
	if err != nil || !strings.HasPrefix(pulled.ID, "sha256:") {
		t.Fatalf("pull: %+v, %v", pulled, err)
	}
	pinned, err := a.pinImage(ctx, app, image)
	if err != nil || pinned != project(app)+":"+shortID(pulled.ID) {
		t.Fatalf("pin: %q, %v", pinned, err)
	}
	compose := "services:\n  web:\n    build: .\n    command: [\"sh\", \"-c\", \"sleep 3600\"]\n    labels:\n      from-commit: \"yes\"\n"
	start := a.newStart(app)
	start.SourceFile = sourceFile(t, map[string]string{"compose.yml": compose, "Dockerfile": "FROM scratch\nRUN false\n"})
	start.Image = pinned
	var log strings.Builder
	if _, err := a.deployHere(ctx, start, &log); err != nil {
		t.Fatalf("deploy: %v\n%s", err, log.String())
	}
	running := func() string {
		t.Helper()
		return strings.TrimSpace(testDocker(t, "", "inspect", "-f", `{{.Config.Image}} {{index .Config.Labels "from-commit"}}`, a.projectOf(app)+"-web-1"))
	}
	if got := running(); got != pinned+" yes" {
		t.Fatalf("after the deploy web runs %q, want %s\n%s", got, pinned, log.String())
	}

	// The follow: the image that runs deploys once more, as the first
	// look of a follow does, then nothing until the tag moves.
	f, err := FollowImageHere(dir, app, image)
	if err != nil {
		t.Fatal(err)
	}
	var alerts []alert.Message
	fw := newFollower(a, dir)
	fw.alert = func(_ context.Context, message alert.Message) { alerts = append(alerts, message) }
	f = fw.look(ctx, f)
	if f.ImageID != pulled.ID || a.current(app) != 2 {
		t.Fatalf("the first look: %+v, release %d, alerts %+v", f, a.current(app), alerts)
	}
	f = fw.look(ctx, f)
	if a.current(app) != 2 {
		t.Fatalf("the same image deployed again: release %d", a.current(app))
	}

	pushVersion(t, image, "2")
	f = fw.look(ctx, f)
	if f.ImageID == pulled.ID || a.current(app) != 3 || len(alerts) != 0 {
		t.Fatalf("after a new image: %+v, release %d, alerts %+v", f, a.current(app), alerts)
	}
	if got, want := running(), project(app)+":"+shortID(f.ImageID)+" yes"; got != want {
		t.Fatalf("web runs %q, want %q", got, want)
	}
	if version := strings.TrimSpace(testDocker(t, "", "inspect", "-f", `{{index .Config.Labels "version"}}`, a.projectOf(app)+"-web-1")); version != "2" {
		t.Fatalf("web runs version %q of the image, want 2", version)
	}

	// The image before it is still on the server, for a rollback.
	if _, err := a.deployHere(ctx, protocol.DeployStart{DeployID: "back", App: app, FromNumber: 2, Domains: start.Domains}, &log); err != nil {
		t.Fatalf("roll back: %v\n%s", err, log.String())
	}
	if got := running(); got != pinned+" yes" {
		t.Fatalf("after the rollback web runs %q, want %s", got, pinned)
	}

	// A tag nobody can pull any more alerts, and deploys nothing.
	f.Image = strings.Replace(image, ":main", ":gone", 1)
	f = fw.look(ctx, f)
	if len(alerts) != 1 || !strings.Contains(alerts[0].Title, "cannot read the image") || a.current(app) != 4 {
		t.Fatalf("a tag that is gone: alerts %+v, release %d", alerts, a.current(app))
	}
}

// A private registry refuses the server until `moin registry set` logged
// it in. The login is in Docker's config, the pull reads it there, and
// `moin registry rm` takes it out again.
func TestPrivateRegistry(t *testing.T) {
	ctx := context.Background()
	config := t.TempDir()
	t.Setenv("DOCKER_CONFIG", config)
	// ada with the token secret-token, as bcrypt.
	registry := startRegistry(t, "ada:$2a$05$.HWLDssIQT4y2s0lfuF5iO7A71kqYzH1gEkZzfgwafUjZTbYA3/Ny\n")
	image := registry + "/ada/shop:1.4"
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", image).Run() })

	if _, err := PullHere(ctx, image); err == nil {
		t.Fatal("a pull without a login worked")
	}
	if err := SetRegistryHere(ctx, Registry{Registry: registry, User: "ada", Token: "wrong"}); err == nil {
		t.Fatal("a wrong token was taken")
	}
	if list, _ := RegistriesHere(); len(list) != 0 {
		t.Fatalf("a login that failed was kept: %+v", list)
	}
	if err := SetRegistryHere(ctx, Registry{Registry: registry, User: "ada", Token: "secret-token"}); err != nil {
		t.Fatal(err)
	}
	list, err := RegistriesHere()
	if err != nil || len(list) != 1 || list[0] != (Registry{Registry: registry, User: "ada"}) {
		t.Fatalf("the logins: %+v, %v", list, err)
	}
	if info, err := os.Stat(filepath.Join(config, "config.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config.json is not for its owner only: %v, %v", info, err)
	}
	pushVersion(t, image, "1")
	pulled, err := PullHere(ctx, image)
	if err != nil || pulled.ID == "" {
		t.Fatalf("a pull with the login: %+v, %v", pulled, err)
	}

	if err := RemoveRegistryHere(ctx, registry); err != nil {
		t.Fatal(err)
	}
	if list, _ := RegistriesHere(); len(list) != 0 {
		t.Fatalf("after the logout: %+v", list)
	}
	testDocker(t, "", "image", "rm", image)
	if _, err := PullHere(ctx, image); err == nil {
		t.Fatal("a pull after the logout worked")
	}
}
