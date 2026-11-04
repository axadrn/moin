package mcp_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/mcp"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
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

// TestAlertDestinationTools: add a webhook, list it without its URL, send
// it a test signed with the secret shown once, and remove it.
func TestAlertDestinationTools(t *testing.T) {
	env := newEnvironment(t)
	ada := seedOwner(t, env, "ada")
	token, _ := createToken(t, env, ada, "read", "write")
	session := connectMemory(t, env.server, ada, token)
	var mu sync.Mutex
	var body, signature string
	fail := false
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		body, signature = string(got), r.Header.Get("X-Moin-Signature")
		if fail {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer hook.Close()

	added := output[mcp.CreatedAlertDestination](t, call(t, session, "add_alert_destination", map[string]any{"kind": "webhook", "name": "ci", "url": hook.URL + "/secret-path"}))
	if added.Name != "ci" || len(added.SigningSecret) != 64 || len(added.Events) != len(model.AlertEvents) {
		t.Fatalf("add_alert_destination = %+v", added)
	}
	if fields := toolError(t, call(t, session, "add_alert_destination", map[string]any{"kind": "telegram", "name": "phone", "bot_token": "nope", "chat_id": "1"}), "validation_failed").Fields; fields["bot_token"] == "" {
		t.Fatalf("fields = %v", fields)
	}
	list := output[mcp.AlertDestinationList](t, call(t, session, "list_alert_destinations", nil))
	if len(list.Items) != 1 || strings.Contains(list.Items[0].Hint, "secret-path") {
		t.Fatalf("list_alert_destinations = %+v", list.Items)
	}
	tested := output[mcp.AlertDestination](t, call(t, session, "test_alert_destination", map[string]any{"name": "ci"}))
	mu.Lock()
	if tested.LastSentAt == nil || !strings.Contains(body, `"event":"test"`) || signature != service.Signature(added.SigningSecret, []byte(body)) {
		t.Fatalf("test: %+v, body %s, signature %q", tested, body, signature)
	}
	fail = true
	mu.Unlock()
	toolError(t, call(t, session, "test_alert_destination", map[string]any{"name": "ci"}), "alert_send_failed")

	readToken, _ := createToken(t, env, ada, "read")
	reader := connectMemory(t, env.server, ada, readToken)
	output[mcp.AlertDestinationList](t, call(t, reader, "list_alert_destinations", nil))
	toolError(t, call(t, reader, "remove_alert_destination", map[string]any{"name": "ci"}), "insufficient_scope")

	output[mcp.Done](t, call(t, session, "remove_alert_destination", map[string]any{"name": "ci"}))
	toolError(t, call(t, session, "remove_alert_destination", map[string]any{"name": "ci"}), "not_found")
	activity := output[mcp.Activity](t, call(t, session, "list_activity", nil))
	if len(activity.Items) < 4 || activity.Items[0].Summary != "removed the Webhook alert destination ci" {
		t.Fatalf("activity = %+v", activity.Items)
	}
}
