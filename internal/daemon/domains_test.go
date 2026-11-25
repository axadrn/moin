package daemon

import (
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestRouteServices: domains per service come from the hub; an entry for
// the main service is not another service; a domain on two services fails.
func TestRouteServices(t *testing.T) {
	domains := &protocol.Domains{Domains: []string{"shop.example.net"}, Services: map[string][]string{"web": {"x.example.net"}, "api": {"API.shop.example.net"}, "admin": {}}}
	detected, err := route("shop", protocol.Detected{Service: "web"}, domains)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(detected.Domains, []string{"shop.example.net"}) || len(detected.ServiceDomains) != 1 || !slices.Equal(detected.ServiceDomains["api"], []string{"api.shop.example.net"}) {
		t.Errorf("domains %v, services %v", detected.Domains, detected.ServiceDomains)
	}
	if _, err := route("shop", protocol.Detected{Service: "web"}, &protocol.Domains{Domains: []string{"x.example.com"}, Services: map[string][]string{"api": {"x.example.com"}}}); err == nil {
		t.Error("a domain on two services passed")
	}
	// The hub's settings are not changed by route.
	if domains.Services["api"][0] != "API.shop.example.net" {
		t.Errorf("route changed the domains it was given: %v", domains.Services)
	}
}

// TestRouteInstant: the hub's provider names the instant address; off, an
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
// of its own, the instant address; both ask for the app's one password.
// Another service gets its own router and port, and the same password.
// Without a password no router asks for one.
func TestDomainsByService(t *testing.T) {
	config := map[string]any{}
	web := map[string]any{}
	addRouters(config, web, routes{app: "shop", router: project("shop"), domains: []string{"shop.example.net"}, port: 3000, instant: "shop.203-0-113-7.sslip.io", protect: "moin:$2y$05$abc"})
	labels := web["labels"].(map[string]any)
	for key, want := range map[string]string{
		"traefik.http.routers.moin-shop.rule":                      "Host(`shop.example.net`)",
		"traefik.http.routers.moin-shop.middlewares":               "moin-shop-auth",
		"traefik.http.routers.moin-shop-tunnel.middlewares":        "moin-shop-auth",
		"traefik.http.routers.moin-shop-instant.rule":              "Host(`shop.203-0-113-7.sslip.io`)",
		"traefik.http.routers.moin-shop-instant.service":           "moin-shop",
		"traefik.http.routers.moin-shop-instant.middlewares":       "moin-shop-auth",
		"traefik.http.middlewares.moin-shop-auth.basicauth.users":  "moin:$$2y$$05$$abc",
		"traefik.http.services.moin-shop.loadbalancer.server.port": "3000",
	} {
		if labels[key] != want {
			t.Errorf("%s = %v, want %s", key, labels[key], want)
		}
	}
	// Off: no router asks for a password.
	open := map[string]any{}
	addRouters(config, open, routes{app: "shop", router: project("shop"), domains: []string{"shop.example.net"}, port: 3000, instant: "shop.203-0-113-7.sslip.io"})
	labels = open["labels"].(map[string]any)
	for key := range labels {
		if strings.Contains(key, "middlewares") {
			t.Errorf("off, yet %s = %v", key, labels[key])
		}
	}
	// An app with only its instant address has no router for domains, and
	// its password still guards the instant address.
	only := map[string]any{}
	addRouters(config, only, routes{app: "idea", router: project("idea"), port: 8080, instant: "idea.203-0-113-7.sslip.io", protect: "moin:$2y$05$abc"})
	labels = only["labels"].(map[string]any)
	if labels["traefik.http.routers.moin-idea.rule"] != nil || labels["traefik.http.routers.moin-idea-instant.rule"] != "Host(`idea.203-0-113-7.sslip.io`)" || labels["traefik.http.routers.moin-idea-instant.middlewares"] != "moin-idea-auth" || labels["traefik.http.middlewares.moin-idea-auth.basicauth.users"] != "moin:$$2y$$05$$abc" {
		t.Errorf("instant only: %v", labels)
	}
	api := map[string]any{}
	addRouters(config, api, routes{app: "shop", router: serviceRouter("shop", "api"), domains: []string{"api.shop.example.net"}, port: 9000, protect: "me:$2y$05$x"})
	labels = api["labels"].(map[string]any)
	if labels["traefik.http.routers.moin-shop-svc-api.rule"] != "Host(`api.shop.example.net`)" || labels["traefik.http.services.moin-shop-svc-api.loadbalancer.server.port"] != "9000" || labels["traefik.http.routers.moin-shop-svc-api.middlewares"] != "moin-shop-svc-api-auth" {
		t.Errorf("api labels: %v", labels)
	}
	if config["networks"].(map[string]any)["default"].(map[string]any)["name"] != "moin-shop_default" {
		t.Errorf("networks: %v", config["networks"])
	}
	if port := servicePort("", map[string]any{"expose": []any{"9000"}}); port != 9000 {
		t.Errorf("service port from expose: %d", port)
	}
}
