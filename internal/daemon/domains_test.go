package daemon

import (
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
	// No canonical domain, so no router has a middleware.
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
	api := map[string]any{}
	addRouters(config, api, routes{app: "shop", router: serviceRouter("shop", "api"), domains: []string{"api.shop.example.net"}, port: 9000})
	labels = api["labels"].(map[string]any)
	if labels["traefik.http.routers.moin-shop-svc-api.rule"] != "Host(`api.shop.example.net`)" || labels["traefik.http.services.moin-shop-svc-api.loadbalancer.server.port"] != "9000" {
		t.Errorf("api labels: %v", labels)
	}
	if config["networks"].(map[string]any)["default"].(map[string]any)["name"] != "moin-shop_default" {
		t.Errorf("networks: %v", config["networks"])
	}
	if port := servicePort("", map[string]any{"expose": []any{"9000"}}); port != 9000 {
		t.Errorf("service port from expose: %d", port)
	}
}
