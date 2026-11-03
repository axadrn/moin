package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/client"
)

// m and :metrics load the metrics of the selected app or server.
func TestMetricsKeyLoadsMetrics(t *testing.T) {
	for _, tab := range []string{"apps", "servers"} {
		m, cmd := frames()[tab].Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
		if cmd == nil || m.(model).err != nil {
			t.Errorf("%s: m starts nothing", tab)
		}
		if m := command(frames()[tab], "metrics"); m.err != nil {
			t.Errorf("%s: :metrics: %v", tab, m.err)
		}
	}
}

func TestMetricsOutputAndRefresh(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	output := appMetricsOutput(7, client.AppMetrics{App: "shop", Server: "web-1", ServerOnline: true,
		Now:     &client.AppMetricsNow{CPU: 12.5, MemoryUsed: 300 << 20, MemoryLimit: 512 << 20, NetRx: 2048},
		Points:  []client.MetricsPoint{{At: at, CPU: 1}, {At: at, CPU: 9}},
		Deploys: []client.DeployMark{{Number: 12, At: at}}})
	text := strings.Join(output.lines, "\n")
	for _, want := range []string{"12.5%", "300 MB of 512 MB", "2.0 KB/s", "▁█", "v12 went live 12:00"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in %s", want, text)
		}
	}
	server := serverMetricsOutput(8, client.ServerMetrics{Server: "web-1", Online: true,
		Now:  &client.ServerMetricsNow{CPU: 42, Cores: 4, MemoryUsed: 1 << 30, MemoryTotal: 4 << 30, DiskUsed: 10 << 30, DiskTotal: 40 << 30},
		Apps: []client.ServerAppMetrics{{App: "shop", Now: &client.AppMetricsNow{MemoryUsed: 300 << 20}}, {App: "blog"}}})
	text = strings.Join(server.lines, "\n")
	for _, want := range []string{"42.0%, 4 cores", "10 GB of 40 GB, 25%", "shop", "not running"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in %s", want, text)
		}
	}

	// The panel refreshes while it shows the metrics, and stops after
	// another output replaced them.
	m := frames()["apps"]
	m, cmd := m.metricsUpdate(metricsLoadedMsg{output: output, app: "shop"})
	if m.outputID != 7 || cmd == nil || !strings.Contains(m.outputTitle, "shop") {
		t.Fatalf("loaded: id %d, cmd %v", m.outputID, cmd)
	}
	m.outputScroll = 3
	if again, _ := m.metricsUpdate(metricsLoadedMsg{output: output, app: "shop"}); again.outputScroll != 3 {
		t.Error("a refresh scrolled the panel back")
	}
	if _, cmd := m.metricsUpdate(metricsRefreshMsg{id: 7, app: "shop"}); cmd == nil {
		t.Error("no refresh while the panel shows the metrics")
	}
	m.outputID = 9
	if _, cmd := m.metricsUpdate(metricsRefreshMsg{id: 7, app: "shop"}); cmd != nil {
		t.Error("a refresh after another output")
	}
	if late, cmd := m.metricsUpdate(metricsLoadedMsg{output: output}); cmd != nil || late.outputID != 9 {
		t.Error("late metrics replaced a newer output")
	}
}
