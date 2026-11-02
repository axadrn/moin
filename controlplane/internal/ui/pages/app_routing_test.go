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
