package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/mcp"
)

// TestUpdateTools: get_version tells of the newest release, only the admin
// of the control plane updates it, and set_daemon_updates turns automatic
// daemon updates off.
func TestUpdateTools(t *testing.T) {
	env := newEnvironment(t)
	if err := env.services.Updates.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.updates, "updater"), []byte("alive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ada := seedOwner(t, env, "ada")
	adaToken, _ := createToken(t, env, ada, "read", "write")
	adaSession := connectMemory(t, env.server, ada, adaToken)
	bob := seedOwner(t, env, "bob")
	bobToken, _ := createToken(t, env, bob, "read", "write")
	bobSession := connectMemory(t, env.server, bob, bobToken)

	version := output[mcp.VersionOutput](t, call(t, bobSession, "get_version", nil))
	if version.Latest != "v0.3.0" || !version.UpdateAvailable || version.CanUpdate || !version.AutoUpdateDaemons || version.Summary != "Updates in one click." {
		t.Fatalf("bob's version = %+v", version)
	}
	if result := call(t, bobSession, "update_control_plane", nil); !result.IsError || !strings.Contains(text(result), `"forbidden"`) {
		t.Fatalf("bob's update = %s", text(result))
	}
	updated := output[mcp.VersionOutput](t, call(t, adaSession, "update_control_plane", nil))
	if updated.Update == nil || updated.Update.State != "requested" || updated.Update.Version != "v0.3.0" {
		t.Fatalf("ada's update = %+v", updated)
	}
	if _, err := os.Stat(filepath.Join(env.updates, "request")); err != nil {
		t.Fatal(err)
	}

	off := output[mcp.DaemonUpdatesOutput](t, call(t, adaSession, "set_daemon_updates", map[string]any{"auto": false}))
	if off.Auto {
		t.Fatalf("set_daemon_updates = %+v", off)
	}
	if version := output[mcp.VersionOutput](t, call(t, adaSession, "get_version", nil)); version.AutoUpdateDaemons {
		t.Fatal("daemons still update by themselves")
	}
}
