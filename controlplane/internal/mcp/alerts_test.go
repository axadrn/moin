package mcp_test

import (
	"testing"

	"github.com/axadrn/ops/controlplane/internal/mcp"
)

// TestEmailAlertsTool: whoami says the alerts are on until set_email_alerts
// turns them off.
func TestEmailAlertsTool(t *testing.T) {
	env := newEnvironment(t)
	ada := seedOwner(t, env, "ada")
	token, _ := createToken(t, env, ada, "read", "write")
	session := connectMemory(t, env.server, ada, token)

	if who := output[mcp.WhoAmI](t, call(t, session, "whoami", nil)); !who.User.EmailAlerts {
		t.Fatalf("whoami = %+v", who.User)
	}
	if off := output[mcp.EmailAlertsOutput](t, call(t, session, "set_email_alerts", map[string]any{"on": false})); off.On {
		t.Fatalf("set_email_alerts = %+v", off)
	}
	if who := output[mcp.WhoAmI](t, call(t, session, "whoami", nil)); who.User.EmailAlerts {
		t.Fatal("email alerts still on")
	}
	readToken, _ := createToken(t, env, ada, "read")
	if result := call(t, connectMemory(t, env.server, ada, readToken), "set_email_alerts", map[string]any{"on": true}); !result.IsError {
		t.Fatal("a read-only token turned the alerts on")
	}
}
