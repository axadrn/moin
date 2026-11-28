package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestCatalogSource: a catalog deploy takes its compose file from the
// catalog built into the daemon, and nothing for a name it does not have.
func TestCatalogSource(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	if err := catalogSource("umami", src); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(src, "compose.yml"))
	if err != nil || !strings.Contains(string(body), "umami") {
		t.Fatalf("compose.yml = %q, %v", body, err)
	}
	for _, name := range []string{"no-such-app", "../etc", "Umami", ""} {
		if err := catalogSource(name, filepath.Join(t.TempDir(), "src")); err == nil {
			t.Errorf("took %q", name)
		}
	}
}

// TestCatalogAddress: a catalog app knows its address from the first
// start: its first domain, else its instant address. An address that is
// still the instant address follows the first domain; one you set stays.
func TestCatalogAddress(t *testing.T) {
	a := newApps(t.TempDir())
	instant := &protocol.Domains{Instant: "sslip.io", InstantIP: "203.0.113.7"}
	if err := a.catalogEnv("umami", instant); err != nil {
		t.Fatal(err)
	}
	env, _ := a.env("umami")
	if env["APP_URL"] != "https://umami.203-0-113-7.sslip.io" || env["APP_DOMAIN"] != "umami.203-0-113-7.sslip.io" || env["VERSION"] != "latest" {
		t.Fatalf("instant: %v", env)
	}
	// moin deploy --catalog umami --domain, or moin domains add.
	if err := a.addDomainLine("umami", "Stats.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := a.addDomainLine("umami", "stats.example.com"); err != nil {
		t.Fatalf("the same domain again: %v", err)
	}
	routing := a.routing("umami")
	routing.InstantIP = "203.0.113.7"
	if err := a.catalogEnv("umami", routing); err != nil {
		t.Fatal(err)
	}
	env, _ = a.env("umami")
	if env["APP_URL"] != "https://stats.example.com" || env["APP_DOMAIN"] != "stats.example.com" {
		t.Fatalf("domain: %v", env)
	}
	a.setEnv(protocol.EnvSet{App: "umami", Set: map[string]string{"APP_URL": "https://mine.example.com"}})
	os.WriteFile(a.domainsFile("umami"), []byte("other.example.com\n"), 0o600)
	a.catalogEnv("umami", a.routing("umami"))
	env, _ = a.env("umami")
	if env["APP_URL"] != "https://mine.example.com" || env["APP_DOMAIN"] != "stats.example.com" {
		t.Fatalf("set by hand: %v", env)
	}
	// Another app has it already.
	os.MkdirAll(a.appDir("blog"), 0o700)
	if err := a.addDomainLine("blog", "other.example.com"); err == nil || !strings.Contains(err.Error(), "umami") {
		t.Errorf("a domain of another app: %v", err)
	}
}

// TestUpdateApp: moin update <app> is for a catalog app, with a version
// that can be an image tag.
func TestUpdateApp(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir)
	os.MkdirAll(a.releaseDir("shop", 1), 0o700)
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	if _, err := UpdateAppHere(context.Background(), dir, "shop", "", io.Discard); err == nil || !strings.Contains(err.Error(), "not an app of the catalog") {
		t.Errorf("your own app: %v", err)
	}
	os.WriteFile(filepath.Join(a.releaseDir("shop", 1), "deploy.json"), []byte(`{"number":1,"catalog":"umami"}`), 0o600)
	for _, version := range []string{"2.15 ; rm", "-x", "a\nb"} {
		if _, err := UpdateAppHere(context.Background(), dir, "shop", version, io.Discard); err == nil || !strings.Contains(err.Error(), "not a version") {
			t.Errorf("%q: %v", version, err)
		}
	}
	env, _ := a.env("shop")
	if env["VERSION"] != "" {
		t.Errorf("a wrong version was set: %v", env)
	}
}
