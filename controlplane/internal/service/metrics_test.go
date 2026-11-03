package service

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/protocol"
)

// sendSamples has the agent send n samples, one every 15 seconds from at,
// with the server's CPU and the app's memory counting up from base.
func sendSamples(t *testing.T, apps *AppService, servers *ServerService, serverID string, clock *time.Time, n int, base float64) {
	t.Helper()
	for i := range n {
		agentSays(t, servers, serverID, protocol.TypeMetrics, protocol.Metrics{
			At: *clock,
			Server: &protocol.ServerMetrics{CPU: base + float64(i), Cores: 2, MemoryUsed: 1 << 30, MemoryTotal: 4 << 30,
				DiskUsed: 10 << 30, DiskTotal: 40 << 30, NetRx: 1000, NetTx: 500, Load: 0.5},
			Apps: []protocol.AppMetrics{
				{App: "shop", CPU: 5, MemoryUsed: uint64(base+float64(i)) << 20, MemoryLimit: 512 << 20, NetRx: 100, NetTx: 50, Containers: 1},
				{App: "Not An App", CPU: 1},
			},
		})
		*clock = clock.Add(protocol.MetricsEvery)
	}
}

func TestMetricsRingMinutesAndHours(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	app, err := apps.Create(owner, "shop", server.Name)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	apps.now = func() time.Time { return clock }

	// Three minutes of samples; one that comes too fast is dropped.
	sendSamples(t, apps, servers, server.ID, &clock, 12, 10)
	clock = clock.Add(-10 * time.Second)
	agentSays(t, servers, server.ID, protocol.TypeMetrics, protocol.Metrics{Server: &protocol.ServerMetrics{CPU: 99}})
	clock = clock.Add(10 * time.Second)
	_, metrics, err := apps.ServerMetrics(owner.WorkspaceID, server.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Range.Name != "1h" || len(metrics.Points) != 12 || metrics.Now == nil || metrics.Now.CPU != 21 || metrics.Now.DiskTotal != 40<<30 {
		t.Fatalf("server metrics = %+v, %d points", metrics.Now, len(metrics.Points))
	}
	if len(metrics.Apps) != 1 || metrics.Apps[0].Name != "shop" || metrics.Apps[0].Now == nil || metrics.Apps[0].Now.MemoryLimit != 512<<20 {
		t.Fatalf("apps on the server = %+v", metrics.Apps)
	}

	// The finished minutes go to SQLite, the one being added up stays.
	apps.FlushMetrics(false)
	start := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC).Unix()
	rows, err := apps.apps.MetricsMinutes(server.ID, start, start+3600)
	if err != nil {
		t.Fatal(err)
	}
	// Minute 12:00 averages 10, 11, 12, 13: 11.5 in tenths.
	if len(rows) != 3 || rows[0].CPU != 115 || rows[0].Memory != 1<<30 || rows[0].Load != 50 || rows[0].NetRx != 1000 {
		t.Fatalf("server minutes = %+v", rows)
	}
	appRows, err := apps.apps.MetricsMinutes(app.ID, start, start+3600)
	if err != nil || len(appRows) != 3 || appRows[0].Memory != int64(11.5*(1<<20)) || appRows[0].CPU != 50 {
		t.Fatalf("app minutes = %+v, %v", appRows, err)
	}

	// A restart forgets the ring; the last hour comes from the minutes.
	apps.metrics = newMetricsStore()
	_, after, err := apps.AppMetrics(owner.WorkspaceID, "shop", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Points) != 3 || after.Now != nil || after.Points[0].Memory != uint64(11.5*(1<<20)) {
		t.Fatalf("app after a restart = %+v", after)
	}

	// Two hours later the first hour is rolled up and 7 days show it.
	sendSamples(t, apps, servers, server.ID, &clock, 4, 50)
	clock = clock.Add(2 * time.Hour)
	sendSamples(t, apps, servers, server.ID, &clock, 4, 70)
	apps.FlushMetrics(false)
	hours, err := apps.apps.MetricsHours(server.ID, start, start+3600)
	if err != nil || len(hours) != 1 {
		t.Fatalf("hours = %+v, %v", hours, err)
	}
	_, week, err := apps.ServerMetrics(owner.WorkspaceID, server.ID, "7d")
	if err != nil {
		t.Fatal(err)
	}
	if len(week.Points) != 2 || week.Points[0].At.Unix() != start || week.Points[1].CPU < 70 {
		t.Fatalf("7 days = %+v", week.Points)
	}
	_, day, err := apps.ServerMetrics(owner.WorkspaceID, server.ID, "24h")
	if err != nil || len(day.Points) != 2 || day.Points[0].At.Unix() != start {
		t.Fatalf("24 hours = %+v, %v", day.Points, err)
	}

	_, _, err = apps.AppMetrics(owner.WorkspaceID, "shop", "2h")
	if !errors.Is(err, ErrMetricsRange) {
		t.Fatalf("range 2h: %v", err)
	}
}

func TestMetricsMarkDeploysThatWentLive(t *testing.T) {
	apps, _, server, _ := newAppService(t)
	app, err := apps.Create(owner, "shop", server.Name)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	apps.now = func() time.Time { return now }
	for _, status := range []string{model.DeploySucceeded, model.DeployFailed} {
		deploy := &model.Deploy{ID: status, AppID: app.ID, Status: model.DeployRunning, CreatedAt: now.Add(-time.Hour)}
		if err := apps.apps.CreateDeploy(deploy); err != nil {
			t.Fatal(err)
		}
		if err := apps.apps.FinishDeploy(deploy.ID, status, "", "", "", now.Add(-30*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	_, metrics, err := apps.AppMetrics(owner.WorkspaceID, "shop", "24h")
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Deploys) != 1 || metrics.Deploys[0].Number != 1 || len(metrics.Points) != 0 || metrics.Now != nil {
		t.Fatalf("metrics = %+v", metrics)
	}
	_, metrics, _ = apps.AppMetrics(owner.WorkspaceID, "shop", "1h")
	if len(metrics.Deploys) != 1 {
		t.Fatalf("1h deploys = %+v", metrics.Deploys)
	}
}

func TestAverageSteps(t *testing.T) {
	samples := []metricsSample{{at: 0, v: metricsValues{10}}, {at: 60, v: metricsValues{20}}, {at: 300, v: metricsValues{40}}}
	steps := averageSteps(samples, 300)
	if len(steps) != 2 || steps[0].at != 0 || steps[0].v[0] != 15 || steps[1].at != 300 || steps[1].v[0] != 40 {
		t.Fatalf("steps = %+v", steps)
	}
	if percent(math.NaN()) != 0 || percent(250) != 100 || nonNegative(math.Inf(1)) != 0 || nonNegative(-3) != 0 {
		t.Fatal("clamps")
	}
}
