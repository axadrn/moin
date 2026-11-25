package mcp_test

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/mcp"
)

// TestGetComposeFile: get_compose_file answers not_live before the first
// deploy, no_compose_file for a live deploy whose daemon sent none, and
// the file of the live deploy, with a read token.
func TestGetComposeFile(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "composer")
	upgrade(t, env, owner)
	writer, _ := createToken(t, env, owner, "read", "write")
	session := connectMemory(t, env.server, owner, writer)
	server := output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "server-1"}))
	output[mcp.CreatedApp](t, call(t, session, "create_app", map[string]any{"app": "blog", "server": server.Server.ID}))

	reader, _ := createToken(t, env, owner, "read")
	session = connectMemory(t, env.server, owner, reader)
	toolError(t, call(t, session, "get_compose_file", map[string]any{"app": "blog"}), "not_live")
	env.database.MustExec(`INSERT INTO deploys (id, app_id, number, status) SELECT 'd1', id, 1, 'succeeded' FROM apps WHERE name = 'blog'`)
	toolError(t, call(t, session, "get_compose_file", map[string]any{"app": "blog"}), "no_compose_file")
	text := "name: blog\nservices:\n  web:\n    image: ghcr.io/me/blog:1\n"
	env.database.MustExec(`INSERT INTO deploys (id, app_id, number, status, compose_file) SELECT 'd2', id, 2, 'succeeded', $1 FROM apps WHERE name = 'blog'`, text)
	file := output[mcp.ComposeFile](t, call(t, session, "get_compose_file", map[string]any{"app": "blog"}))
	if file.Number != 2 || file.Text != text {
		t.Fatalf("get_compose_file = %+v", file)
	}
	if summary := (mcp.ComposeFile{ComposeFile: file.ComposeFile}).Summary(); !strings.Contains(summary, "image: ghcr.io/me/blog:1") {
		t.Fatalf("summary = %q", summary)
	}
}
