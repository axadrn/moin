package mcp_test

import (
	"strings"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/mcp"
)

func TestReorderAppsAndServers(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "order")
	upgrade(t, env, owner)
	token, _ := createToken(t, env, owner, "read", "write")
	session := connectMemory(t, env.server, owner, token)

	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "box-2"}))
	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "box-1"}))
	output[mcp.App](t, call(t, session, "create_app", map[string]any{"app": "shop", "server": "box-1"}))
	output[mcp.App](t, call(t, session, "create_app", map[string]any{"app": "blog", "server": "box-1"}))
	names := func(list mcp.AppList) string {
		var names []string
		for _, app := range list.Items {
			names = append(names, app.Name)
		}
		return strings.Join(names, ",")
	}
	// New apps go to the end, not by name.
	if got := names(output[mcp.AppList](t, call(t, session, "list_apps", map[string]any{}))); got != "shop,blog" {
		t.Fatalf("list_apps = %s", got)
	}
	if got := names(output[mcp.AppList](t, call(t, session, "reorder_apps", map[string]any{"order": []string{"blog", "shop"}}))); got != "blog,shop" {
		t.Fatalf("reorder_apps = %s", got)
	}
	if got := names(output[mcp.AppList](t, call(t, session, "list_apps", map[string]any{}))); got != "blog,shop" {
		t.Fatalf("list_apps after reorder = %s", got)
	}
	if fields := toolError(t, call(t, session, "reorder_apps", map[string]any{"order": []string{"blog"}}), "validation_failed").Fields; fields["order"] == "" {
		t.Fatalf("an order that misses an app: %v", fields)
	}
	toolError(t, call(t, session, "reorder_apps", map[string]any{"order": []string{"blog", "blog"}}), "validation_failed")
	servers := output[mcp.ServerOrder](t, call(t, session, "reorder_servers", map[string]any{"order": []string{"box-1", "box-2"}}))
	if servers.Summary() != "The servers in their order: box-1, box-2." {
		t.Fatalf("reorder_servers = %s", servers.Summary())
	}
	list := output[mcp.ServerList](t, call(t, session, "list_servers", map[string]any{}))
	if len(list.Items) != 2 || list.Items[0].Name != "box-1" {
		t.Fatalf("list_servers after reorder = %+v", list.Items)
	}
	// The same order again changes nothing and records nothing.
	output[mcp.ServerOrder](t, call(t, session, "reorder_servers", map[string]any{"order": []string{"box-1", "box-2"}}))
	activity := output[mcp.Activity](t, call(t, session, "list_activity", map[string]any{}))
	reorders := 0
	for _, event := range activity.Items {
		if strings.HasPrefix(event.Summary, "put the ") {
			reorders++
		}
	}
	if reorders != 2 {
		t.Fatalf("%d reorders in the activity, want 2", reorders)
	}
}
