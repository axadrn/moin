package pages

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// Every domain offers remove and the page offers add: moin keeps them all.
func TestDomainsOfferChanges(t *testing.T) {
	view := DomainsView{App: model.App{Name: "shop"}, Checks: []service.DomainCheck{{Domain: "shop.example.com", Canonical: true, Points: true}}}
	var html strings.Builder
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "Remove shop.example.com") || !strings.Contains(html.String(), "data-add-domain") {
		t.Errorf("a domain lost its actions")
	}
}

// The instant address shows with its provider and no password of its
// own. A domain of another service shows its service and no canonical
// action.
func TestDomainsInstantAndServices(t *testing.T) {
	view := DomainsView{
		App:     model.App{Name: "shop"},
		Checks:  []service.DomainCheck{{Domain: "shop.example.com"}, {Domain: "www.shop.example.com", Canonical: true}, {Domain: "api.shop.example.com", Service: "api"}},
		Instant: service.Instant{Provider: "sslip.io", Host: "shop.203-0-113-7.sslip.io", URL: "https://shop.203-0-113-7.sslip.io"},
	}
	var html strings.Builder
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{"https://shop.203-0-113-7.sslip.io", "data-instant-provider", `data-domain-service="api"`, "Remove api.shop.example.com", "canonical", `/apps/shop/domains/remove"`, `id="app-domains"`} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s", want)
		}
	}
	if strings.Contains(page, "instant-password") || strings.Contains(page, "instant-new") {
		t.Errorf("the instant address has a password of its own")
	}
	if !strings.Contains(page, "data-canonical") || strings.Contains(page, `value="api.shop.example.com"`) {
		t.Errorf("no canonical choice of the main domains")
	}
	if strings.Contains(page, "data-domain-checked") {
		t.Errorf("a domain shows a DNS answer it was never asked for")
	}

	view.Instant = service.Instant{Provider: "sslip.io", Reason: "The server runs behind a Cloudflare Tunnel."}
	html.Reset()
	if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "data-instant-reason") || strings.Contains(html.String(), "data-instant-url") {
		t.Errorf("no reason for the missing address")
	}
}

// The password is one switch for every address: off says anyone gets in,
// on who signs in; a password moin just made shows once. The fields set
// your own.
func TestDomainsPasswordSwitch(t *testing.T) {
	render := func(view DomainsView) string {
		t.Helper()
		var html strings.Builder
		if err := AppDomains(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
			t.Fatal(err)
		}
		return html.String()
	}
	off := render(DomainsView{App: model.App{Name: "shop"}})
	if !strings.Contains(off, "data-password-switch") || strings.Contains(off, "checked") || !strings.Contains(off, "Anyone with the link") || !strings.Contains(off, "Use your own password") {
		t.Errorf("off: %s", off)
	}
	on := render(DomainsView{App: model.App{Name: "shop", Protect: "ada:$2a$10$hash"}})
	if !strings.Contains(on, "checked") || !strings.Contains(on, "Sign in as ada") || strings.Contains(on, "data-new-password") || strings.Contains(on, "unprotect") {
		t.Errorf("on: %s", on)
	}
	made := render(DomainsView{App: model.App{Name: "shop", Protect: "moin:$2a$10$hash"}, NewPassword: "fresh-password-123"})
	if !strings.Contains(made, "fresh-password-123") || !strings.Contains(made, "Shown once.") || !strings.Contains(made, "data-new-password") {
		t.Errorf("the new password is not shown once: %s", made)
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

// The overview lists the app's domains, like the Domains page.
func TestOverviewListsTheDomains(t *testing.T) {
	app := model.App{Name: "my-app", LiveNumber: 3, Domains: []model.Domain{{Name: "my-app.example.com", Main: true}}}
	var html strings.Builder
	if err := appHomeSection(AppPageView{App: app}).Render(projectPageContext("/app/tiny-rocket-co/apps/my-app"), &html); err != nil {
		t.Fatal(err)
	}
	if page := html.String(); !strings.Contains(page, `href="https://my-app.example.com"`) || strings.Contains(page, "No domain yet") {
		t.Errorf("the overview lacks the domain: %s", page)
	}
}
