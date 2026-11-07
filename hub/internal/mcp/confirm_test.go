package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/hub/internal/mcp"
)

// TestRemoveToolsNeedTheName: remove_app, remove_bucket and remove_server
// answer confirm_mismatch without the name or with another one, the same
// check as the web and the API, and go with the name typed out.
func TestRemoveToolsNeedTheName(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "confirm")
	upgrade(t, env, owner)
	token, _ := createToken(t, env, owner, "read", "write")
	session := connectMemory(t, env.server, owner, token)
	bucket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer bucket.Close()

	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "box-1"}))
	output[mcp.App](t, call(t, session, "create_app", map[string]any{"app": "shop", "server": "box-1"}))
	output[mcp.Bucket](t, call(t, session, "add_bucket", map[string]any{"name": "spare", "endpoint": bucket.URL, "bucket": "spare", "access_key": "k", "secret_key": "s"}))

	// The app goes first: the bucket is the default every app uses.
	for _, thing := range []struct{ tool, field, name string }{
		{"remove_app", "app", "shop"},
		{"remove_bucket", "bucket", "spare"},
		{"remove_server", "server", "box-1"},
	} {
		for _, confirm := range []string{"", "blog"} {
			toolError(t, call(t, session, thing.tool, map[string]any{thing.field: thing.name, "confirm": confirm}), "confirm_mismatch")
		}
		if result := call(t, session, thing.tool, map[string]any{thing.field: thing.name, "confirm": thing.name}); result.IsError {
			t.Fatalf("%s with the name: %s", thing.tool, text(result))
		}
	}
}
