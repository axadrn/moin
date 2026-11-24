package model

import "testing"

// Two ways to write one image name the same image; the tag and the digest
// are not part of it.
func TestImageName(t *testing.T) {
	for ref, want := range map[string]string{
		"nginx":                             "docker.io/library/nginx",
		"nginx:1.27":                        "docker.io/library/nginx",
		"docker.io/library/nginx@sha256:ab": "docker.io/library/nginx",
		"index.docker.io/me/shop:v2":        "docker.io/me/shop",
		"me/shop:3f2a9c1":                   "docker.io/me/shop",
		"ghcr.io/Me/Shop:abc":               "ghcr.io/me/shop",
		"ghcr.io/me/shop":                   "ghcr.io/me/shop",
		"registry.example.com:5000/shop:1":  "registry.example.com:5000/shop",
		"localhost:5000/shop":               "localhost:5000/shop",
		"localhost/shop:dev":                "localhost/shop",
		"":                                  "",
	} {
		if got := ImageName(ref); got != want {
			t.Errorf("ImageName(%q) = %q, want %q", ref, got, want)
		}
	}
}

// One row from compose.yml says the app's domains come from there: rows
// of compose.yml and rows entered in moin never mix.
func TestDomainsFromCompose(t *testing.T) {
	app := App{Domains: []Domain{{Name: "my-app.example.com", Main: true}}}
	if app.DomainsFromCompose() {
		t.Error("domains entered in moin count as from compose.yml")
	}
	app.Domains = append(app.Domains, Domain{Service: "api", Name: "api.example.com", FromCompose: true})
	if !app.DomainsFromCompose() {
		t.Error("a domain from compose.yml does not count")
	}
}

// The domains split by service: the main service's with their
// canonical one, the others by service.
func TestDomainsOf(t *testing.T) {
	app := App{Domains: []Domain{{Name: "shop.example.com"}, {Name: "www.shop.example.com", Main: true}, {Service: "api", Name: "api.shop.example.com"}}}
	if names, canonical := app.DomainsOf(""); len(names) != 2 || names[1] != "www.shop.example.com" || canonical != "www.shop.example.com" {
		t.Errorf("main = %v, %q", names, canonical)
	}
	if names, canonical := app.DomainsOf("api"); len(names) != 1 || canonical != "" {
		t.Errorf("api = %v, %q", names, canonical)
	}
	if services := app.ServiceDomains(); len(services) != 1 || services["api"][0] != "api.shop.example.com" {
		t.Errorf("services = %v", services)
	}
}
