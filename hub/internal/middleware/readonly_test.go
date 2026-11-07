package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadOnlyAllows(t *testing.T) {
	for _, test := range []struct {
		pattern, path string
		allowed       bool
	}{
		{"GET /app/{workspaceSlug}/apps/{app}", "/app/acme/apps/blog", true},
		{"POST /app/{workspaceSlug}/apps/{app}/stop", "/app/acme/apps/blog/stop", true},
		{"POST /api/v1/apps/{app}/start", "/api/v1/apps/blog/start", true},
		{"POST /app/{workspaceSlug}/billing/checkout", "/app/acme/billing/checkout", true},
		{"POST /app/{workspaceSlug}/workspace/invitations", "/app/acme/workspace/invitations", true},
		{"DELETE /api/v1/workspaces/{workspaceSlug}", "/api/v1/workspaces/acme", true},
		{"PATCH /app/{workspaceSlug}/account/email", "/app/acme/account/email", true},
		{"POST /app/{workspaceSlug}/settings/api-tokens", "/app/acme/settings/api-tokens", true},
		{"POST /api/v1/apps/{app}/databases/query", "/api/v1/apps/blog/databases/query", true},
		{"DELETE /api/v1/apps/{app}/hook", "/api/v1/apps/blog/hook", true},
		{"POST /api/v1/alert-destinations", "/api/v1/alert-destinations", true},
		{"POST /api/v1/alert-destinations/{destination}/test", "/api/v1/alert-destinations/team/test", true},
		{"POST /app/{workspaceSlug}/alerts/destinations", "/app/acme/alerts/destinations", true},
		{"POST /api/v1/apps/{app}/restart", "/api/v1/apps/blog/restart", false},
		{"POST /app/{workspaceSlug}/apps/{app}/redeploy", "/app/acme/apps/blog/redeploy", false},
		{"POST /api/v1/servers", "/api/v1/servers", false},
		{"PATCH /api/v1/apps/{app}/env", "/api/v1/apps/blog/env", false},
		{"POST /app/{workspaceSlug}/settings/registries", "/app/acme/settings/registries", false},
		{"POST /api/v1/apps/{app}/deploys", "/api/v1/apps/stop/deploys", false},
		{"DELETE /api/v1/apps/{app}", "/api/v1/apps/stop", false},
		{"DELETE /app/{workspaceSlug}/apps/{app}", "/app/acme/apps/start", false},
		// Names that look like the routes a read-only workspace keeps.
		{"POST /api/v1/apps/{app}/deploys", "/api/v1/apps/accounting/deploys", false},
		{"PATCH /api/v1/apps/{app}/env", "/api/v1/apps/account/env", false},
		{"POST /app/{workspaceSlug}/apps/{app}/redeploy", "/app/workspace/apps/blog/redeploy", false},
		{"POST /app/{workspaceSlug}/apps/{app}/image", "/app/acme/apps/billing/image", false},
		{"POST /app/{workspaceSlug}/servers", "/app/profile/servers", false},
	} {
		got := false
		mux := http.NewServeMux()
		mux.HandleFunc(test.pattern, func(_ http.ResponseWriter, r *http.Request) { got = readOnlyAllows(r) })
		method, _, _ := strings.Cut(test.pattern, " ")
		request := httptest.NewRequest(method, test.path, nil)
		mux.ServeHTTP(httptest.NewRecorder(), request)
		if got != test.allowed {
			t.Errorf("%s %s: allowed = %v, want %v", test.pattern, test.path, got, test.allowed)
		}
	}
	if readOnlyAllows(httptest.NewRequest("POST", "/app/acme/account/email", nil)) {
		t.Error("a change outside a route is allowed")
	}
}
