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

// The domains x-moin.domains sets in the live deploy win over the ones saved
// in moin; without them the saved ones count.
func TestMainDomains(t *testing.T) {
	app := App{Domains: `["old.example.com"]`, Canonical: "old.example.com"}
	if domains, canonical := app.MainDomains(); len(domains) != 1 || domains[0] != "old.example.com" || canonical != "old.example.com" {
		t.Fatalf("saved domains = %v, %q", domains, canonical)
	}
	app.LiveDetected = `{"service":"web","domains":["my-app.example.com","www.my-app.example.com"],"canonical":"my-app.example.com","config":{"domains":["my-app.example.com","www.my-app.example.com"]}}`
	if domains, canonical := app.MainDomains(); len(domains) != 2 || domains[0] != "my-app.example.com" || canonical != "my-app.example.com" {
		t.Fatalf("compose domains = %v, %q", domains, canonical)
	}
	app.LiveDetected = `{"service":"web","domains":["old.example.com"],"config":{}}`
	if domains, _ := app.ComposeDomains(); domains != nil {
		t.Fatalf("a compose.yml without domains sets %v", domains)
	}
}
