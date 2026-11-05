package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/api"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerMetricsTools registers the tool that reads metrics.
func registerMetricsTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "get_metrics", Title: "Get metrics", Annotations: readOnly("Get metrics"),
		Description: "Returns the metrics of an app or a server: the newest sample and points over a range (1h with every 15 second sample, 24h, 7d or 30d as averages). For an app: CPU as its share of the whole server in percent, memory used and its limit, network in and out in bytes per second, and the deploys that went live in the range, to tell what changed after a deploy. For a server: CPU of all cores in percent, memory, the data disk, network and load, and every app on it now, the one with the most memory first. Name the app or the server, not both. Needs the read scope.",
	}, Read(member(services, tools.getMetrics)))
}

// MetricsInput is the input of get_metrics.
type MetricsInput struct {
	App    string `json:"app,omitempty" jsonschema:"the app name or ID"`
	Server string `json:"server,omitempty" jsonschema:"the server name or ID, instead of an app"`
	Range  string `json:"range,omitempty" jsonschema:"1h (the default), 24h, 7d or 30d"`
}

// Metrics is the output of get_metrics: an app's or a server's.
type Metrics struct {
	App    *api.AppMetrics    `json:"app,omitempty"`
	Server *api.ServerMetrics `json:"server,omitempty"`
}

func (m Metrics) Summary() string {
	if m.App != nil {
		app := m.App
		if app.Now == nil {
			return app.App + " sent no metrics in the last two minutes."
		}
		summary := fmt.Sprintf("%s now: CPU %.1f%%, memory %s", app.App, app.Now.CPU, bytesText(app.Now.MemoryUsed))
		if app.Now.MemoryLimit > 0 {
			summary += " of " + bytesText(app.Now.MemoryLimit)
		}
		summary += "."
		if len(app.Deploys) > 0 {
			last := app.Deploys[len(app.Deploys)-1]
			summary += fmt.Sprintf(" v%d went live at %s.", last.Number, last.At.Format("Jan 2 15:04 UTC"))
		}
		return summary
	}
	if m.Server == nil {
		return ""
	}
	server := m.Server
	if server.Now == nil {
		return server.Server + " sent no metrics in the last two minutes."
	}
	now := server.Now
	summary := fmt.Sprintf("%s now: CPU %.1f%%, memory %s of %s, disk %s of %s.", server.Server, now.CPU,
		bytesText(now.MemoryUsed), bytesText(now.MemoryTotal), bytesText(now.DiskUsed), bytesText(now.DiskTotal))
	apps := []string{}
	for _, app := range server.Apps {
		if app.Now != nil && len(apps) < 3 {
			apps = append(apps, app.App+" "+bytesText(app.Now.MemoryUsed))
		}
	}
	if len(apps) > 0 {
		summary += " Most memory: " + strings.Join(apps, ", ") + "."
	}
	return summary
}

// bytesText is a size for people: 512 MB, 1.5 GB.
func bytesText(value uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 || size >= 10 {
		return fmt.Sprintf("%.0f %s", size, units[unit])
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func (t moinTools) getMetrics(_ context.Context, caller Caller, input MetricsInput) (Metrics, error) {
	switch {
	case input.App != "" && input.Server != "":
		return Metrics{}, InvalidField("server", "Name the app or the server, not both.")
	case input.Server != "":
		server, metrics, err := t.services.Apps.ServerMetrics(caller.Token.WorkspaceID, input.Server, input.Range)
		if err != nil {
			return Metrics{}, err
		}
		answer := api.NewServerMetrics(server, metrics)
		return Metrics{Server: &answer}, nil
	case input.App == "":
		return Metrics{}, InvalidField("app", "Name an app or a server.")
	}
	app, metrics, err := t.services.Apps.AppMetrics(caller.Token.WorkspaceID, input.App, input.Range)
	if err != nil {
		return Metrics{}, err
	}
	answer := api.NewAppMetrics(app, metrics)
	return Metrics{App: &answer}, nil
}
