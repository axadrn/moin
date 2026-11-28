package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/axadrn/moin/protocol"
)

// TestPasswordLine: the file holds the user and a bcrypt hash of the
// password, never the password.
func TestPasswordLine(t *testing.T) {
	line, err := passwordLine("ada", "open sesame")
	if err != nil {
		t.Fatal(err)
	}
	user, hash, _ := strings.Cut(strings.TrimSuffix(line, "\n"), ":")
	if user != "ada" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("open sesame")) != nil {
		t.Fatalf("line = %q", line)
	}
	if strings.Contains(line, "open sesame") {
		t.Fatalf("the line holds the password: %q", line)
	}
	if _, err := passwordLine("ada", ""); err == nil {
		t.Error("no password was taken")
	}
	if _, err := passwordLine("ada", strings.Repeat("x", bcryptLimit+1)); err == nil {
		t.Error("a password bcrypt would cut was taken")
	}
}

// TestPasswordUser: a colon would end the user in the file.
func TestPasswordUser(t *testing.T) {
	for user, want := range map[string]bool{"moin": true, "ada@example.com": true, "a:b": false, "a b": false, "": false} {
		if got := passwordUser.MatchString(user); got != want {
			t.Errorf("%q: %v, want %v", user, got, want)
		}
	}
}

// TestPasswordOnEveryRouter: the domains, the tunnel and the instant
// address all ask for the password, www after its redirect.
func TestPasswordOnEveryRouter(t *testing.T) {
	service := map[string]any{}
	addRouters(map[string]any{}, service, routes{
		app: "shop", router: "moin-shop", port: 8080,
		domains:   []string{"example.com", "www.example.com"},
		instant:   "shop.203-0-113-7.sslip.io",
		usersFile: "/apps/shop/releases/3/htpasswd",
	})
	labels := service["labels"].(map[string]any)
	if got := labels["traefik.http.middlewares.moin-shop-auth.basicauth.usersfile"]; got != "/apps/shop/releases/3/htpasswd" {
		t.Fatalf("the users file = %v", got)
	}
	for router, want := range map[string]string{"moin-shop": "moin-shop-www,moin-shop-auth", "moin-shop-tunnel": "moin-shop-https,moin-shop-www,moin-shop-auth", "moin-shop-instant": "moin-shop-auth"} {
		if got := labels["traefik.http.routers."+router+".middlewares"]; got != want {
			t.Errorf("%s: middlewares = %v, want %s", router, got, want)
		}
	}

	open := map[string]any{}
	addRouters(map[string]any{}, open, routes{app: "shop", router: "moin-shop", port: 8080, domains: []string{"example.com"}, instant: "shop.203-0-113-7.sslip.io"})
	for key := range open["labels"].(map[string]any) {
		if strings.Contains(key, "auth") || strings.HasSuffix(key, ".middlewares") && !strings.Contains(key, "-tunnel.") {
			t.Errorf("an app without a password has %s", key)
		}
	}
}

// TestReleaseGetsItsOwnPasswordFile: the proxy reads a password file
// once, so every release names its own copy of the app's.
func TestReleaseGetsItsOwnPasswordFile(t *testing.T) {
	a := newApps(t.TempDir())
	line := "ada:$2a$10$hash\n"
	if err := writePassword(a.passwordFile("shop"), []byte(line)); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(a.passwordFile("shop")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the password file: %v %v", info, err)
	}
	finished := func(number int) map[string]any {
		t.Helper()
		dir := a.releaseDir("shop", number)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		start := protocol.DeployStart{App: "shop", Number: number, Domains: &protocol.Domains{Domains: []string{"shop.example.com"}}}
		release := &release{start: start, dir: dir, config: map[string]any{"services": map[string]any{"web": map[string]any{"image": "shop:1"}}}, composeJSON: filepath.Join(dir, "compose.json"), passwordFile: a.passwordFile("shop")}
		release.detected, _ = route("shop", protocol.Detected{Service: "web", Port: 8080}, start.Domains)
		if err := finish(release); err != nil {
			t.Fatal(err)
		}
		var config struct {
			Services map[string]struct {
				Labels map[string]any `json:"labels"`
			} `json:"services"`
		}
		body, _ := os.ReadFile(release.composeJSON)
		if err := json.Unmarshal(body, &config); err != nil {
			t.Fatal(err)
		}
		return config.Services["web"].Labels
	}
	labels := finished(3)
	if got := labels["traefik.http.middlewares.moin-shop-auth.basicauth.usersfile"]; got != "/apps/shop/releases/3/htpasswd" {
		t.Fatalf("the users file = %v", got)
	}
	if copied, _ := os.ReadFile(filepath.Join(a.releaseDir("shop", 3), "htpasswd")); string(copied) != line {
		t.Fatalf("the release's copy = %q", copied)
	}

	// Without the file the next release asks for nothing.
	if err := writePassword(a.passwordFile("shop"), nil); err != nil {
		t.Fatal(err)
	}
	for key := range finished(4) {
		if strings.Contains(key, "auth") {
			t.Errorf("no password, but %s", key)
		}
	}
	if exists(filepath.Join(a.releaseDir("shop", 4), "htpasswd")) {
		t.Error("a release without a password has a password file")
	}
}

// TestDropPassword: a compose file to run elsewhere asks for no password,
// as its file stays here, and keeps the www redirect.
func TestDropPassword(t *testing.T) {
	service := map[string]any{}
	addRouters(map[string]any{}, service, routes{app: "shop", router: "moin-shop", port: 8080, domains: []string{"example.com", "www.example.com"}, instant: "shop.203-0-113-7.sslip.io", usersFile: "/apps/shop/releases/3/htpasswd"})
	dropPassword("shop", map[string]any{"services": map[string]any{"web": service}})
	labels := service["labels"].(map[string]any)
	for key, value := range labels {
		if strings.Contains(key, "auth") || strings.Contains(value.(string), "auth") {
			t.Errorf("still there: %s = %v", key, value)
		}
	}
	if got := labels["traefik.http.routers.moin-shop.middlewares"]; got != "moin-shop-www" {
		t.Errorf("the www redirect = %v", got)
	}
	if _, ok := labels["traefik.http.routers.moin-shop-instant.middlewares"]; ok {
		t.Error("the instant router keeps an empty list of middlewares")
	}
}
