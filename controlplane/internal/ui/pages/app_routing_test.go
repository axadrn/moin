package pages

import (
	"strings"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/service"
)

// Domains x-ops sets are marked and offer no remove, canonical or add,
// because ops cannot change them.
func TestRoutingMarksComposeDomains(t *testing.T) {
	view := RoutingView{App: model.App{Name: "shop"}, Checks: []service.DomainCheck{{Domain: "shop.de", Canonical: true, Points: true, FromCompose: true}}}
	var html strings.Builder
	if err := AppRouting(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	if !strings.Contains(page, "from compose.yml") || !strings.Contains(page, "data-domains-from-compose") {
		t.Errorf("no compose marker: %s", page)
	}
	if strings.Contains(page, "Remove shop.de") || strings.Contains(page, "data-add-domain") {
		t.Errorf("compose domains offer changes: %s", page)
	}

	view.Checks[0].FromCompose = false
	html.Reset()
	if err := AppRouting(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "Remove shop.de") || !strings.Contains(html.String(), "data-add-domain") {
		t.Errorf("ops domains lost their actions")
	}
}

// The instant address shows with its password hidden, and a domain of
// another service shows its service and no canonical action.
func TestRoutingInstantAndServices(t *testing.T) {
	view := RoutingView{
		App:     model.App{Name: "shop"},
		Checks:  []service.DomainCheck{{Domain: "shop.de", Points: true}, {Domain: "api.shop.de", Service: "api", Points: true}},
		Instant: service.Instant{Provider: "sslip.io", URL: "https://shop.203-0-113-7.sslip.io", User: "ops"},
	}
	var html strings.Builder
	if err := AppRouting(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{"https://shop.203-0-113-7.sslip.io", "data-instant-show", `data-domain-service="api"`, "Remove api.shop.de"} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s", want)
		}
	}
	if strings.Count(page, "Make canonical") != 1 {
		t.Errorf("a service domain offers canonical")
	}

	view.Instant = service.Instant{Provider: "sslip.io", Reason: "The server runs behind a Cloudflare Tunnel."}
	html.Reset()
	if err := AppRouting(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "data-instant-reason") || strings.Contains(html.String(), "data-instant-url") {
		t.Errorf("no reason for the missing address")
	}
}
