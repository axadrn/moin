package mcp_test

import (
	"testing"

	"github.com/axadrn/moin/hub/internal/mcp"
)

func TestUptimeTools(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "uptime")
	upgrade(t, env, owner)
	token, _ := createToken(t, env, owner, "read", "write")
	session := connectMemory(t, env.server, owner, token)

	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "box-1"}))
	output[mcp.App](t, call(t, session, "create_app", map[string]any{"app": "shop", "server": "box-1"}))
	output[mcp.Change](t, call(t, session, "set_domains", map[string]any{"app": "shop", "domains": []string{"shop.de"}}))

	uptime := output[mcp.Uptime](t, call(t, session, "get_uptime", map[string]any{"app": "shop"}))
	if uptime.State != "pending" || len(uptime.Domains) != 1 || uptime.Domains[0].Path != "/" || uptime.Domains[0].Uptime24h != nil {
		t.Fatalf("get_uptime = %+v", uptime)
	}
	domain := output[mcp.UptimeDomain](t, call(t, session, "set_uptime", map[string]any{"app": "shop", "domain": "shop.de", "path": "/health", "paused": true}))
	if domain.Path != "/health" || !domain.CustomPath || !domain.Paused || domain.State != "paused" {
		t.Fatalf("set_uptime = %+v", domain)
	}
	fields := toolError(t, call(t, session, "set_uptime", map[string]any{"app": "shop", "domain": "other.de", "paused": false}), "validation_failed").Fields
	if fields["domain"] == "" {
		t.Fatalf("fields = %v", fields)
	}
	activity := output[mcp.Activity](t, call(t, session, "list_activity", nil))
	if len(activity.Items) < 2 || activity.Items[0].Summary != "paused uptime of shop.de of shop" {
		t.Fatalf("activity = %+v", activity.Items)
	}

	readOnly, _ := createToken(t, env, owner, "read")
	reader := connectMemory(t, env.server, owner, readOnly)
	output[mcp.Uptime](t, call(t, reader, "get_uptime", map[string]any{"app": "shop"}))
	toolError(t, call(t, reader, "set_uptime", map[string]any{"app": "shop", "domain": "shop.de", "paused": false}), "insufficient_scope")
}
