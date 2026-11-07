package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// The uptime page shows each domain with its state, numbers and outages,
// and the server when it is offline.
func TestUptimeShowsEveryDomain(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	started, ended := now.Add(-2*time.Hour), now.Add(-90*time.Minute)
	full, some := 100.0, 99.5
	view := UptimeView{App: model.App{Name: "shop", ServerName: "web-1"}, Now: now, Uptime: service.AppUptime{
		State: model.UptimeDown, Server: "web-1",
		Domains: []service.UptimeDomain{
			{Domain: "shop.de", Path: "/health", State: model.UptimeUp, Uptime24h: &full, Uptime7d: &some},
			{Domain: "www.shop.de", Path: "/", State: model.UptimeDown, Error: "HTTP 502", Outages: []model.UptimeOutage{{StartedAt: started, EndedAt: &ended, Reason: "HTTP 502"}}},
		},
	}}
	var html strings.Builder
	if err := AppUptime(view).Render(projectPageContext("/app/tiny-rocket-co-workspace1/apps/shop/uptime"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{`data-uptime-domain="shop.de"`, `data-uptime-domain="www.shop.de"`, `placeholder="/health"`, "100%", "99.50%", "down for 30m · HTTP 502", "data-server-offline", `data-uptime-state="down"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page has no %q", want)
		}
	}
}
