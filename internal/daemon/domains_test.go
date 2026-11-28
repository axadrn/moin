package daemon

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestRouteInstant: the settings' provider names the instant address; off, an
// unknown provider or no public IPv4 give none.
func TestRouteInstant(t *testing.T) {
	for _, test := range []struct{ provider, ip, want string }{
		{"sslip.io", "203.0.113.7", "shop.203-0-113-7.sslip.io"},
		{"nip.io", "203.0.113.7", "shop.203-0-113-7.nip.io"},
		{"off", "203.0.113.7", ""},
		{"sslip.io", "", ""},
		{"sslip.io", "2a01:4f8::1", ""},
	} {
		detected, err := route("shop", protocol.Detected{Service: "web"}, &protocol.Domains{Instant: test.provider, InstantIP: test.ip})
		if err != nil || detected.Instant != test.want {
			t.Errorf("%+v: instant %q, err %v", test, detected.Instant, err)
		}
	}
}

// TestDomainsByService: the main service routes its domains and, on a router
// of its own, the instant address. Another service gets its own router
// and port.
func TestDomainsByService(t *testing.T) {
	config := map[string]any{}
	web := map[string]any{}
	addRouters(config, web, routes{app: "shop", router: project("shop"), domains: []string{"shop.example.net"}, port: 3000, instant: "shop.203-0-113-7.sslip.io"})
	labels := web["labels"].(map[string]any)
	for key, want := range map[string]string{
		"traefik.http.routers.moin-shop.rule":                      "Host(`shop.example.net`)",
		"traefik.http.routers.moin-shop-instant.rule":              "Host(`shop.203-0-113-7.sslip.io`)",
		"traefik.http.routers.moin-shop-instant.service":           "moin-shop",
		"traefik.http.services.moin-shop.loadbalancer.server.port": "3000",
	} {
		if labels[key] != want {
			t.Errorf("%s = %v, want %s", key, labels[key], want)
		}
	}
	// Only the tunnel router has a middleware: it sends http to https.
	for key := range labels {
		if strings.Contains(key, "middlewares") && !strings.Contains(key, "-https") && !strings.Contains(key, "-tunnel.") {
			t.Errorf("%s = %v", key, labels[key])
		}
	}
	if labels["traefik.http.routers.moin-shop-tunnel.middlewares"] != "moin-shop-https" || labels["traefik.http.middlewares.moin-shop-https.redirectscheme.scheme"] != "https" {
		t.Errorf("tunnel: %v", labels)
	}
	// An app with only its instant address has no router for domains.
	only := map[string]any{}
	addRouters(config, only, routes{app: "idea", router: project("idea"), port: 8080, instant: "idea.203-0-113-7.sslip.io"})
	labels = only["labels"].(map[string]any)
	if labels["traefik.http.routers.moin-idea.rule"] != nil || labels["traefik.http.routers.moin-idea-instant.rule"] != "Host(`idea.203-0-113-7.sslip.io`)" {
		t.Errorf("instant only: %v", labels)
	}
	if config["networks"].(map[string]any)["default"].(map[string]any)["name"] != "moin-shop_default" {
		t.Errorf("networks: %v", config["networks"])
	}
	if port := servicePort("", map[string]any{"expose": []any{"9000"}}); port != 9000 {
		t.Errorf("service port from expose: %d", port)
	}
}

// TestWwwRedirectsToTheApex: an app with example.com and www.example.com
// sends www to example.com, path and query kept, on the HTTPS router and
// on the tunnel's. A www domain without its apex is served as it is.
func TestWwwRedirectsToTheApex(t *testing.T) {
	web := map[string]any{}
	addRouters(map[string]any{}, web, routes{app: "shop", router: project("shop"), domains: []string{"example.com", "www.example.com", "www.other.example.net", "shop.example.org"}, port: 3000, instant: "shop.203-0-113-7.sslip.io"})
	labels := web["labels"].(map[string]any)
	for router, want := range map[string]string{"moin-shop": "moin-shop-www", "moin-shop-tunnel": "moin-shop-https,moin-shop-www"} {
		if got := labels["traefik.http.routers."+router+".middlewares"]; got != want {
			t.Errorf("%s middlewares = %v, want %s", router, got, want)
		}
	}
	if got := labels["traefik.http.routers.moin-shop-instant.middlewares"]; got != nil {
		t.Errorf("the instant address redirects: %v", got)
	}
	if labels["traefik.http.middlewares.moin-shop-www.redirectregex.permanent"] != "true" {
		t.Errorf("the redirect is not permanent: %v", labels)
	}
	// compose reads $$ as $; Traefik matches with Go's regexp.
	unescape := func(key string) string {
		return strings.ReplaceAll(labels["traefik.http.middlewares.moin-shop-www.redirectregex."+key].(string), "$$", "$")
	}
	pattern := regexp.MustCompile(unescape("regex"))
	for from, want := range map[string]string{
		"https://www.example.com/cart?id=7":     "https://example.com/cart?id=7",
		"http://www.example.com:8080/":          "https://example.com/",
		"https://example.com/cart":              "https://example.com/cart",
		"https://www.other.example.net/":        "https://www.other.example.net/",
		"https://www.example.com.evil.test/":    "https://www.example.com.evil.test/",
		"https://shop.example.org/":             "https://shop.example.org/",
		"https://www.example.com":               "https://example.com",
		"https://wwwxexample.com/":              "https://wwwxexample.com/",
		"https://www.example.com/www.example.c": "https://example.com/www.example.c",
	} {
		got := from
		if pattern.MatchString(from) {
			got = pattern.ReplaceAllString(from, unescape("replacement"))
		}
		if got != want {
			t.Errorf("%s goes to %s, want %s", from, got, want)
		}
	}
	// Without the pair, no redirect.
	one := map[string]any{}
	addRouters(map[string]any{}, one, routes{app: "blog", router: project("blog"), domains: []string{"www.example.com"}, port: 3000})
	for key := range one["labels"].(map[string]any) {
		if strings.Contains(key, "-www") {
			t.Errorf("www alone redirects: %s", key)
		}
	}
}

// TestServiceDomains: a line "domain service" in the domains file sends
// the domain to another service than the main one, on a router of its own
// and the service's own port.
func TestServiceDomains(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir)
	os.MkdirAll(a.releaseDir("shop", 1), 0o700)
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	os.WriteFile(a.domainsFile("shop"), []byte("shop.example.com\napi.example.com api\n"), 0o600)
	routing := a.routing("shop")
	routing.InstantIP = ""
	if strings.Join(routing.Domains, " ") != "shop.example.com" || strings.Join(routing.Services["api"], " ") != "api.example.com" {
		t.Fatalf("routing = %+v", routing)
	}
	detected, err := route("shop", protocol.Detected{Service: "web"}, routing)
	if err != nil || strings.Join(detected.ServiceDomains["api"], " ") != "api.example.com" {
		t.Fatalf("route: %+v, %v", detected, err)
	}
	if got := strings.Join(a.reachable(detected), " "); got != "shop.example.com api.example.com" {
		t.Errorf("reachable = %q", got)
	}
	// A domain reaches one service.
	if _, err := route("shop", protocol.Detected{Service: "web"}, &protocol.Domains{Domains: []string{"api.example.com"}, Services: routing.Services}); err == nil {
		t.Error("one domain for two services")
	}
	// A line that names the main service is one of its domains.
	main, _ := route("shop", protocol.Detected{Service: "web"}, &protocol.Domains{Services: map[string][]string{"web": {"www.example.com"}}})
	if strings.Join(main.Domains, " ") != "www.example.com" || main.ServiceDomains != nil {
		t.Errorf("main by name: %+v", main)
	}

	config := map[string]any{"services": map[string]any{
		"web": map[string]any{"image": "shop:1"},
		"api": map[string]any{"image": "shop-api:1", "expose": []any{"4000"}},
	}}
	file := filepath.Join(a.releaseDir("shop", 1), "compose.json")
	if err := finish(&release{start: protocol.DeployStart{App: "shop", Number: 1}, config: config, detected: detected, composeJSON: file}); err != nil {
		t.Fatal(err)
	}
	var written struct {
		Services map[string]struct {
			Labels map[string]string `json:"labels"`
		} `json:"services"`
	}
	body, _ := os.ReadFile(file)
	json.Unmarshal(body, &written)
	api := written.Services["api"].Labels
	if api["traefik.http.routers.moin-shop-svc-api.rule"] != "Host(`api.example.com`)" || api["traefik.http.services.moin-shop-svc-api.loadbalancer.server.port"] != "4000" {
		t.Errorf("api labels: %v", api)
	}
	if written.Services["web"].Labels["traefik.http.routers.moin-shop.rule"] != "Host(`shop.example.com`)" {
		t.Errorf("web labels: %v", written.Services["web"].Labels)
	}

	// Adding a domain checks the service is in the compose file.
	if _, err := AddDomainHere(context.Background(), dir, "shop", "admin.example.com", "admin", io.Discard); err == nil || !strings.Contains(err.Error(), "api, web") {
		t.Errorf("an unknown service: %v", err)
	}
	// A deploy fails when the service is gone, with the way out.
	gone := map[string]any{"services": map[string]any{"web": map[string]any{"image": "shop:2"}}}
	if err := finish(&release{start: protocol.DeployStart{App: "shop", Number: 2}, config: gone, detected: detected, composeJSON: file}); err == nil || !strings.Contains(err.Error(), "domains rm shop api.example.com") {
		t.Errorf("a gone service: %v", err)
	}
}
