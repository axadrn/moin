//go:build docker

package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestAppPassword runs an app behind the real proxy: with a password it
// answers 401 without it and 200 with it, a new password counts from the
// release that starts for it and the old one no longer does, and without
// one everyone is let in again.
func TestAppPassword(t *testing.T) {
	if exec.Command("docker", "inspect", proxyName).Run() == nil {
		t.Skipf("a %s runs on this machine: the test would replace it", proxyName)
	}
	ctx := context.Background()
	defer func(stable, window time.Duration) { stableFor, startWindow = stable, window }(stableFor, startWindow)
	stableFor, startWindow = time.Second, 30*time.Second
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	app := "pw-" + hex.EncodeToString(suffix)
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", app, "down", "--volumes", "--remove-orphans").Run()
		_ = exec.Command("docker", "rm", "-f", proxyName).Run()
		_ = exec.Command("docker", "network", "rm", proxyNetwork).Run()
	})
	a := newApps(t.TempDir())
	a.proxyMode = func() string { return "tunnel" }
	domain := app + ".example.net"
	if err := writeWhole(a.domainsFile(app), []byte(domain+"\n")); err != nil {
		t.Fatal(err)
	}
	start := a.newStart(app)
	start.SourceFile = sourceFile(t, map[string]string{"compose.yml": `services:
  web:
    image: busybox:latest
    command: ["sh", "-c", "mkdir -p /www && echo ok > /www/index.html && httpd -f -p 8080 -h /www"]
`})
	if _, err := a.deployHere(ctx, start, io.Discard); err != nil {
		t.Fatal(err)
	}

	// status is what a visitor with this user and password gets, 0 when
	// the proxy does not answer.
	status := func(user, password string) int {
		out, err := exec.Command("docker", "inspect", "-f", "{{(index .NetworkSettings.Networks \""+proxyNetwork+"\").IPAddress}}", proxyName).Output()
		if err != nil {
			return 0
		}
		request, _ := http.NewRequest(http.MethodGet, "http://"+strings.TrimSpace(string(out))+":"+tunnelPort+"/", nil)
		request.Host = domain
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
	// The proxy takes a moment to see a new release.
	soon := func(what string, user, password string, want int) {
		t.Helper()
		got := 0
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
			if got = status(user, password); got == want {
				return
			}
		}
		t.Fatalf("%s: HTTP %d, want %d", what, got, want)
	}
	set := func(user, password string) {
		t.Helper()
		line, err := passwordLine(user, password)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.changePassword(ctx, app, []byte(line), io.Discard); err != nil {
			t.Fatal(err)
		}
	}

	soon("without a password", "", "", http.StatusOK)

	set("ada", "first")
	if !a.hasPassword(app) {
		t.Fatal("the app has no password file")
	}
	soon("a visitor without the password", "", "", http.StatusUnauthorized)
	soon("a visitor with the password", "ada", "first", http.StatusOK)
	soon("a visitor with a wrong password", "ada", "wrong", http.StatusUnauthorized)
	soon("a visitor with another user", "bob", "first", http.StatusUnauthorized)

	set("ada", "second")
	soon("the new password", "ada", "second", http.StatusOK)
	soon("the old password", "ada", "first", http.StatusUnauthorized)

	if _, err := a.changePassword(ctx, app, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	soon("without a password again", "", "", http.StatusOK)
	if a.hasPassword(app) {
		t.Error("the password file is still there")
	}
}
