package mcp_test

import (
	"strings"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/mcp"
)

// TestStatusTool: get_status answers with a read token; a new app counts,
// nothing needs the user, and the summary says all good.
func TestStatusTool(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "status")
	upgrade(t, env, owner)
	writer, _ := createToken(t, env, owner, "read", "write")
	session := connectMemory(t, env.server, owner, writer)
	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "box-1"}))
	output[mcp.App](t, call(t, session, "create_app", map[string]any{"app": "shop", "server": "box-1"}))

	reader, _ := createToken(t, env, owner, "read")
	status := output[mcp.Status](t, call(t, connectMemory(t, env.server, owner, reader), "get_status", map[string]any{}))
	if !status.OK || status.Counts.Apps != 1 || status.Counts.Servers != 1 {
		t.Fatalf("get_status = %+v", status)
	}
	if summary := status.Summary(); !strings.HasPrefix(summary, "All good: 0 of 1 apps live") {
		t.Errorf("summary = %q", summary)
	}
}
