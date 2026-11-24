package pages

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// Domains x-moin sets are marked and offer no remove, canonical or add,
// because moin cannot change them.
func TestDomainsMarkComposeDomains(t *testing.T) {
	app := model.App{Name: "shop", Domains: []model.Domain{{Name: "shop.example.com", Main: true, FromCompose: true}}}
	view := DomainsView{App: app, Checks: []service.DomainCheck{{Domain: "shop.example.com", Canonical: true, Points: true, FromCompose: true}}}
	var html strings.Builder
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	if !strings.Contains(page, "from compose.yml") || !strings.Contains(page, "data-domains-from-compose") {
		t.Errorf("no compose marker: %s", page)
	}
	if strings.Contains(page, "Remove shop.example.com") || strings.Contains(page, "data-add-domain") {
		t.Errorf("compose domains offer changes: %s", page)
	}

	// Domains compose.yml sets only for another service lock the main
	// service too: never a mix.
	html.Reset()
	api := DomainsView{App: model.App{Name: "shop", Domains: []model.Domain{{Service: "api", Name: "api.example.com", FromCompose: true}}}, Checks: []service.DomainCheck{{Domain: "api.example.com", Service: "api", FromCompose: true}}}
	if err := AppDomains(api).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html.String(), "data-add-domain") {
		t.Errorf("service domains from compose.yml offer to add a domain")
	}

	view.App.Domains[0].FromCompose = false
	view.Checks[0].FromCompose = false
	html.Reset()
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "Remove shop.example.com") || !strings.Contains(html.String(), "data-add-domain") {
		t.Errorf("moin domains lost their actions")
	}
}

// The instant address shows without its password, which moin never shows
// again, with New password; a new one shows once. A domain of another
// service shows its service and no canonical action.
func TestDomainsInstantAndServices(t *testing.T) {
	view := DomainsView{
		App:     model.App{Name: "shop"},
		Checks:  []service.DomainCheck{{Domain: "shop.example.com"}, {Domain: "www.shop.example.com", Canonical: true}, {Domain: "api.shop.example.com", Service: "api"}},
		Instant: service.Instant{Provider: "sslip.io", Host: "shop.203-0-113-7.sslip.io", URL: "https://shop.203-0-113-7.sslip.io", User: "moin"},
	}
	var html strings.Builder
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{"https://shop.203-0-113-7.sslip.io", "data-instant-new", `data-domain-service="api"`, "Remove api.shop.example.com", "canonical", `/apps/shop/domains/remove"`, `/apps/shop/domains/instant-new-password"`, `id="app-domains"`} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s", want)
		}
	}
	if !strings.Contains(page, "data-canonical") || strings.Contains(page, `value="api.shop.example.com"`) {
		t.Errorf("no canonical choice of the main domains")
	}
	if strings.Contains(page, "data-domain-checked") {
		t.Errorf("a domain shows a DNS answer it was never asked for")
	}
	if strings.Contains(page, "data-instant-password") {
		t.Errorf("a password shows that nobody just made")
	}
	view.NewPassword = "fresh-password-123"
	html.Reset()
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "fresh-password-123") || !strings.Contains(html.String(), "Shown once.") || strings.Contains(html.String(), "data-instant-new") {
		t.Errorf("the new password is not shown once:\n%s", html.String())
	}
	view.NewPassword = ""

	view.Instant = service.Instant{Provider: "sslip.io", Reason: "The server runs behind a Cloudflare Tunnel."}
	html.Reset()
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "data-instant-reason") || strings.Contains(html.String(), "data-instant-url") {
		t.Errorf("no reason for the missing address")
	}
}

// A domain added or checked says whether it points at the server, or the
// record to set.
func TestDomainsShowTheCheck(t *testing.T) {
	view := DomainsView{App: model.App{Name: "shop"}, Checks: []service.DomainCheck{
		{Domain: "shop.example.com", Checked: true, Points: true},
		{Domain: "www.shop.example.com", Checked: true, Advice: "A record to 203.0.113.7"},
		{Domain: "old.shop.example.com"},
	}}
	var html strings.Builder
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	if strings.Count(page, "data-domain-checked") != 2 || !strings.Contains(page, "Points here") || !strings.Contains(page, "Set its A record to 203.0.113.7") {
		t.Errorf("no answer of the check: %s", page)
	}
	if !strings.Contains(page, `data-check="old.shop.example.com"`) {
		t.Errorf("no Check button")
	}
}

// The overview lists the domains compose.yml sets, like the Domains page.
func TestOverviewListsComposeDomains(t *testing.T) {
	app := model.App{Name: "my-app", LiveNumber: 3, Domains: []model.Domain{{Name: "my-app.example.com", FromCompose: true}}}
	var html strings.Builder
	if err := appHomeSection(AppPageView{App: app}).Render(projectPageContext("/app/tiny-rocket-co/apps/my-app"), &html); err != nil {
		t.Fatal(err)
	}
	if page := html.String(); !strings.Contains(page, `href="https://my-app.example.com"`) || strings.Contains(page, "No domain yet") {
		t.Errorf("the overview lacks the domain from compose.yml: %s", page)
	}
}
