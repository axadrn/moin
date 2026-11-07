package mcp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/mcp"
	"github.com/axadrn/moin/internal/link"
	"github.com/axadrn/moin/protocol"
)

func TestMetricsTool(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "metrics")
	upgrade(t, env, owner)
	token, _ := createToken(t, env, owner, "read")
	writer, _ := createToken(t, env, owner, "read", "write")
	admin := connectMemory(t, env.server, owner, writer)
	session := connectMemory(t, env.server, owner, token)

	server := output[mcp.CreatedServer](t, call(t, admin, "add_server", map[string]any{"server": "box-1"}))
	output[mcp.App](t, call(t, admin, "create_app", map[string]any{"app": "shop", "server": "box-1"}))

	empty := output[mcp.Metrics](t, call(t, session, "get_metrics", map[string]any{"app": "shop"}))
	if empty.App == nil || empty.App.Now != nil || len(empty.App.Points) != 0 || empty.App.Range != "1h" {
		t.Fatalf("metrics before a sample = %+v", empty.App)
	}

	message, err := protocol.NewMessage(protocol.TypeMetrics, protocol.Metrics{
		At:     time.Now(),
		Server: &protocol.ServerMetrics{CPU: 42, MemoryUsed: 1 << 30, MemoryTotal: 4 << 30, DiskUsed: 5 << 30, DiskTotal: 20 << 30},
		Apps:   []protocol.AppMetrics{{App: "shop", CPU: 12.5, MemoryUsed: 300 << 20, MemoryLimit: 512 << 20, Containers: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	serverID, _ := link.Parse(server.Server.ID)
	env.services.Servers.Receive(serverID, message)

	app := output[mcp.Metrics](t, call(t, session, "get_metrics", map[string]any{"app": "shop"}))
	if app.App == nil || app.App.Now == nil || app.App.Now.CPU != 12.5 || len(app.App.Points) != 1 {
		t.Fatalf("app metrics = %+v", app.App)
	}
	box := output[mcp.Metrics](t, call(t, session, "get_metrics", map[string]any{"server": "box-1", "range": "24h"}))
	if box.Server == nil || box.Server.Now == nil || box.Server.Now.CPU != 42 || box.Server.Range != "24h" || len(box.Server.Apps) != 1 || box.Server.Apps[0].Now == nil {
		t.Fatalf("server metrics = %+v", box.Server)
	}
	if summary := box.Summary(); !strings.Contains(summary, "CPU 42.0%") || !strings.Contains(summary, "shop 300 MB") {
		t.Fatalf("summary = %q", summary)
	}

	fields := toolError(t, call(t, session, "get_metrics", map[string]any{"app": "shop", "range": "2h"}), "validation_failed").Fields
	if fields["range"] == "" {
		t.Fatalf("fields = %v", fields)
	}
	toolError(t, call(t, session, "get_metrics", map[string]any{"app": "shop", "server": "box-1"}), "validation_failed")
	toolError(t, call(t, session, "get_metrics", map[string]any{"app": "nope"}), "not_found")
}
