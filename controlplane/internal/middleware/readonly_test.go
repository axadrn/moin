package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestReadOnlyAllows(t *testing.T) {
	for _, test := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/app/acme/apps/blog", true},
		{"POST", "/app/acme/apps/blog/stop", true},
		{"POST", "/api/v1/apps/blog/start", true},
		{"POST", "/app/acme/billing/checkout", true},
		{"POST", "/app/acme/workspace/invitations", true},
		{"DELETE", "/api/v1/workspaces/acme", true},
		{"PATCH", "/app/acme/account/email", true},
		{"POST", "/app/acme/settings/api-tokens", true},
		{"POST", "/api/v1/apps/blog/databases/query", true},
		{"POST", "/api/v1/apps/blog/restart", false},
		{"POST", "/app/acme/apps/blog/redeploy", false},
		{"POST", "/api/v1/servers", false},
		{"PATCH", "/api/v1/apps/blog/env", false},
		{"POST", "/app/acme/settings/registries", false},
		{"POST", "/api/v1/apps/stop/deploys", false},
		{"DELETE", "/api/v1/apps/stop", false},
		{"DELETE", "/app/acme/apps/start", false},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		if got := readOnlyAllows(request); got != test.allowed {
			t.Errorf("%s %s: allowed = %v, want %v", test.method, test.path, got, test.allowed)
		}
	}
}
