package mcp_test

import (
	"context"
	"testing"

	"github.com/axadrn/moin/hub/internal/mcp"
)

// TestUpdateTools: get_version tells of the newest release, and
// set_daemon_updates turns automatic daemon updates off.
func TestUpdateTools(t *testing.T) {
	env := newEnvironment(t)
	if err := env.services.Updates.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	ada := seedOwner(t, env, "ada")
	adaToken, _ := createToken(t, env, ada, allRights...)
	adaSession := connectMemory(t, env.server, ada, adaToken)

	version := output[mcp.VersionOutput](t, call(t, adaSession, "get_version", nil))
	if version.Latest != "v0.3.0" || !version.UpdateAvailable || version.ReadFirst == "" || !version.AutoUpdateDaemons || version.Summary != "A new release." {
		t.Fatalf("ada's version = %+v", version)
	}

	off := output[mcp.DaemonUpdatesOutput](t, call(t, adaSession, "set_daemon_updates", map[string]any{"auto": false}))
	if off.Auto {
		t.Fatalf("set_daemon_updates = %+v", off)
	}
	if version := output[mcp.VersionOutput](t, call(t, adaSession, "get_version", nil)); version.AutoUpdateDaemons {
		t.Fatal("daemons still update by themselves")
	}
}
