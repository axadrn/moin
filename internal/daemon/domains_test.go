package daemon

import (
	"slices"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestRouteServices: domains per service from the hub and from
// the map form of x-moin, x-moin winning per service; the main service's
// entry of the map is its domains; a domain on two services fails.
func TestRouteServices(t *testing.T) {
	domains := &protocol.Domains{Domains: []string{"shop.example.net"}, Services: map[string][]string{"api": {"API.shop.example.net"}, "admin": {"admin.shop.example.net"}}}
	detected, err := route("shop", protocol.Detected{Service: "web", Config: &protocol.Config{ServiceDomains: map[string][]string{"admin": {"desk.shop.example.net"}}}}, domains)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(detected.Domains, []string{"shop.example.net"}) || !slices.Equal(detected.ServiceDomains["api"], []string{"api.shop.example.net"}) || !slices.Equal(detected.ServiceDomains["admin"], []string{"desk.shop.example.net"}) {
		t.Errorf("domains %v, services %v", detected.Domains, detected.ServiceDomains)
	}
	detected, err = route("shop", protocol.Detected{Service: "web", Config: &protocol.Config{ServiceDomains: map[string][]string{"web": {"x.example.com"}, "api": {"api.x.example.com"}}}}, nil)
	if err != nil || !slices.Equal(detected.Domains, []string{"x.example.com"}) || len(detected.ServiceDomains) != 1 {
		t.Errorf("map form with the main service: %v %v %v", detected.Domains, detected.ServiceDomains, err)
	}
	if _, err := route("shop", protocol.Detected{Service: "web"}, &protocol.Domains{Domains: []string{"x.example.com"}, Services: map[string][]string{"api": {"x.example.com"}}}); err == nil {
		t.Error("a domain on two services passed")
	}
	// The hub's settings are not changed by route.
	if domains.Services["api"][0] != "API.shop.example.net" {
		t.Errorf("route changed the domains it was given: %v", domains.Services)
	}
}

// TestRouteInstant: the hub's provider, x-moin picking another or
// off, an unknown provider failing, and nothing without a public IPv4.
func TestRouteInstant(t *testing.T) {
	for _, test := range []struct {
		provider, xops, ip, want string
		fails                    bool
	}{
		{"sslip.io", "", "203.0.113.7", "shop.203-0-113-7.sslip.io", false},
		{"sslip.io", "nip.io", "203.0.113.7", "shop.203-0-113-7.nip.io", false},
		{"sslip.io", "off", "203.0.113.7", "", false},
		{"", "traefik.me", "203.0.113.7", "", true},
		{"sslip.io", "", "", "", false},
		{"sslip.io", "", "2a01:4f8::1", "", false},
		{"sslip.io", "example.com", "203.0.113.7", "", true},
	} {
		detected, err := route("shop", protocol.Detected{Service: "web", Config: &protocol.Config{Instant: test.xops}}, &protocol.Domains{Instant: test.provider, InstantIP: test.ip})
		if (err != nil) != test.fails || detected.Instant != test.want {
			t.Errorf("%+v: instant %q, err %v", test, detected.Instant, err)
		}
	}
}

// TestDomainsByService: the main service routes its domains and, on a router
// of its own, the instant address with its password; another service gets
// its own router and port, and the app's password.
func TestDomainsByService(t *testing.T) {
	config := map[string]any{}
	web := map[string]any{}
	addRouters(config, web, routes{app: "shop", router: project("shop"), domains: []string{"shop.example.net"}, port: 3000, instant: "shop.203-0-113-7.sslip.io", instantProtect: "moin:$2y$05$abc"})
	labels := web["labels"].(map[string]any)
	for key, want := range map[string]string{
		"traefik.http.routers.moin-shop.rule":                             "Host(`shop.example.net`)",
		"traefik.http.routers.moin-shop-instant.rule":                     "Host(`shop.203-0-113-7.sslip.io`)",
		"traefik.http.routers.moin-shop-instant.service":                  "moin-shop",
		"traefik.http.routers.moin-shop-instant.middlewares":              "moin-shop-instant-auth",
		"traefik.http.middlewares.moin-shop-instant-auth.basicauth.users": "moin:$$2y$$05$$abc",
		"traefik.http.services.moin-shop.loadbalancer.server.port":        "3000",
	} {
		if labels[key] != want {
			t.Errorf("%s = %v, want %s", key, labels[key], want)
		}
	}
	if labels["traefik.http.routers.moin-shop.middlewares"] != nil {
		t.Errorf("own domains got the instant password: %v", labels["traefik.http.routers.moin-shop.middlewares"])
	}
	// An app with only its instant address has no router for domains.
	only := map[string]any{}
	addRouters(config, only, routes{app: "idea", router: project("idea"), port: 8080, instant: "idea.203-0-113-7.sslip.io"})
	labels = only["labels"].(map[string]any)
	if labels["traefik.http.routers.moin-idea.rule"] != nil || labels["traefik.http.routers.moin-idea-instant.rule"] != "Host(`idea.203-0-113-7.sslip.io`)" || labels["traefik.http.routers.moin-idea-instant.middlewares"] != nil {
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
	if port := servicePort("api", map[string]any{"expose": []any{"9000"}}, nil); port != 9000 {
		t.Errorf("service port from expose: %d", port)
	}
	if port := servicePort("api", map[string]any{}, &protocol.Config{Ports: map[string]int{"api": 7000}}); port != 7000 {
		t.Errorf("service port from x-moin: %d", port)
	}
}
