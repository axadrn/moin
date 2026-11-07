package mcp_test

import (
	"testing"

	"github.com/axadrn/moin/hub/internal/mcp"
)

// TestRenameApp: rename_app takes the app by name, ID or link and answers
// it renamed with its ID of 32 characters, which every tool takes.
func TestRenameApp(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "renamer")
	upgrade(t, env, owner)
	token, _ := createToken(t, env, owner, "read", "write")
	session := connectMemory(t, env.server, owner, token)

	server := output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "web-1"}))
	created := output[mcp.App](t, call(t, session, "create_app", map[string]any{"app": "blog", "server": server.Server.ID}))
	if len(created.ID) != 32 || len(created.ServerID) != 32 || created.ServerID != server.Server.ID {
		t.Fatalf("IDs = %q on %q, server %q", created.ID, created.ServerID, server.Server.ID)
	}
	renamed := output[mcp.RenamedApp](t, call(t, session, "rename_app", map[string]any{"app": "blog-" + created.ID, "name": "journal"}))
	if renamed.ID != created.ID || renamed.Name != "journal" {
		t.Fatalf("rename_app = %+v", renamed)
	}
	for _, ref := range []string{"journal", created.ID, "blog-" + created.ID} {
		if got := output[mcp.AppDetail](t, call(t, session, "get_app", map[string]any{"app": ref})); got.App.ID != created.ID {
			t.Fatalf("get_app %s = %+v", ref, got.App)
		}
	}
	toolError(t, call(t, session, "get_app", map[string]any{"app": "blog"}), "not_found")
	toolError(t, call(t, session, "rename_app", map[string]any{"app": "journal", "name": "Not Valid"}), "validation_failed")
	reader, _ := createToken(t, env, owner, "read")
	toolError(t, call(t, connectMemory(t, env.server, owner, reader), "rename_app", map[string]any{"app": "journal", "name": "diary"}), "insufficient_scope")
}
