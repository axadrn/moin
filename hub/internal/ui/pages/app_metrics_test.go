package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// The app's metrics page, under the app, shows the numbers now, a chart per
// metric with the deploy marked, and refreshes itself for the last hour.
func TestAppMetricsShowsChartsAndDeploys(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	view := AppMetricsView{App: model.App{Name: "shop", ServerName: "web-1"}, Metrics: service.AppMetrics{
		Range: service.MetricsRanges[0], Server: "web-1", ServerOnline: true,
		Now: &service.MetricsNow{At: at, CPU: 12.5, MemoryUsed: 300 << 20, MemoryLimit: 512 << 20, NetRx: 2048, Containers: 2},
		Points: []service.MetricsPoint{
			{At: at.Add(-time.Minute), CPU: 10, Memory: 200 << 20},
			{At: at, CPU: 12.5, Memory: 300 << 20},
		},
		Deploys: []model.MetricsDeploy{{Number: 12, FinishedAt: at}},
	}}
	var html strings.Builder
	if err := AppMetricsPage(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop/metrics"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{`data-metrics-chart="app-cpu"`, `data-metrics-chart="app-memory"`, `data-metrics-chart="app-network"`, "12%", "300 MB", "of 512 MB", "Containers", "v12 went live", `hx-trigger="every 15s"`, `name="range" value="30d"`, `href="/app/tiny-rocket-co/apps/shop"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page has no %q", want)
		}
	}

	// Longer ranges do not refresh, and no points is an empty state.
	view.Metrics.Range, view.Metrics.Points, view.Metrics.Now = service.MetricsRanges[2], nil, nil
	html.Reset()
	if err := AppMetricsPage(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop/metrics"), &html); err != nil {
		t.Fatal(err)
	}
	if page := html.String(); strings.Contains(page, "every 15s") || !strings.Contains(page, "No metrics yet") {
		t.Error("7 days refreshes or has no empty state")
	}
}

func TestServerMetricsListsAppsByMemory(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	view := ServerMetricsView{Server: model.Server{ID: "s1", Name: "web-1", Online: true}, Metrics: service.ServerMetrics{
		Range: service.MetricsRanges[1], Online: true,
		Now:    &service.MetricsNow{At: at, CPU: 42, Cores: 4, MemoryUsed: 1 << 30, MemoryTotal: 4 << 30, DiskUsed: 10 << 30, DiskTotal: 40 << 30, Load: 0.5},
		Points: []service.MetricsPoint{{At: at, CPU: 42, Memory: 1 << 30, Disk: 10 << 30}},
		Apps:   []service.AppMetricsNow{{Name: "shop", Now: &service.MetricsNow{CPU: 5, MemoryUsed: 300 << 20}}, {Name: "blog"}},
	}}
	var html strings.Builder
	if err := ServerMetricsPage(view).Render(projectPageContext("/app/tiny-rocket-co/servers/s1/metrics"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{`data-metrics-chart="server-disk"`, "1.0 GB of 4.0 GB", "25% used", "4 cores, load 0.50", `data-app-metrics-row="shop"`, "not running", "/servers/s1/security"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page has no %q", want)
		}
	}
	if strings.Contains(page, "every 15s") {
		t.Error("24 hours refreshes")
	}
}
