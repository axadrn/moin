package daemon

import (
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
	// No router has a middleware.
	for key := range labels {
		if strings.Contains(key, "middlewares") {
			t.Errorf("%s = %v", key, labels[key])
		}
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
	for _, router := range []string{"moin-shop", "moin-shop-tunnel"} {
		if got := labels["traefik.http.routers."+router+".middlewares"]; got != "moin-shop-www" {
			t.Errorf("%s middlewares = %v", router, got)
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
		if strings.Contains(key, "middlewares") {
			t.Errorf("www alone redirects: %s", key)
		}
	}
}
