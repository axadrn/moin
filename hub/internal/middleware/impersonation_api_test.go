package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
)

func TestBlockImpersonatedWritesUnderAPI(t *testing.T) {
	operatorID := "operator"
	impersonation := &model.Session{ImpersonatorUserID: &operatorID}

	for _, test := range []struct {
		method     string
		path       string
		wantCalled bool
	}{
		{http.MethodGet, "/api/v1/account", true},
		{http.MethodGet, "/api/v1/account/sessions", true},
		{http.MethodHead, "/api/v1/billing", true},
		{http.MethodGet, "/api/v1/workspaces/acme/billing", true},
		{http.MethodGet, "/api/v1/billing/portal", false},
		{http.MethodGet, "/api/v1/workspaces/acme/billing/manage", false},
		{http.MethodPost, "/api/v1/billing/checkout", false},
		{http.MethodPatch, "/api/v1/account/name", false},
		{http.MethodDelete, "/api/v1/account", false},
		{http.MethodDelete, "/api/v1/account/sessions", false},
		{http.MethodPost, "/api/v1/admin/users/user-1/sessions/revoke", false},
		{http.MethodPost, "/api/v1/auth/logout", true},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request = request.WithContext(ctxkeys.WithSession(request.Context(), impersonation))
			called := false
			recorder := httptest.NewRecorder()
			BlockImpersonatedWrites(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(recorder, request)

			if called != test.wantCalled {
				t.Fatalf("called = %t, want %t", called, test.wantCalled)
			}
			if test.wantCalled {
				if recorder.Code != http.StatusNoContent {
					t.Fatalf("status = %d, want 204", recorder.Code)
				}
				return
			}
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", recorder.Code)
			}
			if recorder.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("content type = %q", recorder.Header().Get("Content-Type"))
			}
			var body api.ErrorBody
			err := json.Unmarshal(recorder.Body.Bytes(), &body)
			if err != nil {
				t.Fatalf("decode %q: %v", recorder.Body.String(), err)
			}
			if body.Error.Code != "impersonation_read_only" {
				t.Fatalf("code = %q, want impersonation_read_only", body.Error.Code)
			}
		})
	}
}

func TestBlockImpersonatedWritesKeepsOrdinarySessions(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/billing/checkout", nil)
	request = request.WithContext(ctxkeys.WithSession(request.Context(), &model.Session{ID: "session-1"}))
	called := false
	recorder := httptest.NewRecorder()
	BlockImpersonatedWrites(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})).ServeHTTP(recorder, request)
	if !called {
		t.Fatalf("ordinary session was blocked: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestImpersonationCannotConnectGitHub: GitHub's return link stores an
// installation, so a read-only session may not follow it.
func TestImpersonationCannotConnectGitHub(t *testing.T) {
	operatorID := "operator"
	for path, allowed := range map[string]bool{"/app/github/setup": false, "/app/acme/apps": true} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request = request.WithContext(ctxkeys.WithSession(request.Context(), &model.Session{ImpersonatorUserID: &operatorID}))
		if got := allowedDuringImpersonation(request); got != allowed {
			t.Errorf("%s: allowed = %t, want %t", path, got, allowed)
		}
	}
}

// TestImpersonationHidesPrivateData: env values, database rows and app logs
// stay hidden from an operator, on the web and in the API.
func TestImpersonationHidesPrivateData(t *testing.T) {
	operatorID := "operator"
	for path, private := range map[string]bool{
		"/app/acme/apps/blog/env?show=1":              true,
		"/app/acme/apps/blog/env":                     false,
		"/app/acme/apps/blog/logs":                    true,
		"/app/acme/apps/blog/data?db=app.db":          true,
		"/app/acme/apps/blog/backups":                 false,
		"/app/acme/apps/logs":                         false,
		"/app/acme/apps/data/detail":                  false,
		"/api/v1/apps/blog/env":                       true,
		"/api/v1/apps/blog/logs":                      true,
		"/api/v1/apps/blog/databases/rows?table=user": true,
		"/api/v1/apps/blog/databases/tables":          false,
		"/api/v1/apps/blog/backups":                   false,
		"/api/v1/apps/env":                            false,
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request = request.WithContext(ctxkeys.WithSession(request.Context(), &model.Session{ImpersonatorUserID: &operatorID}))
		recorder := httptest.NewRecorder()
		called := false
		BlockImpersonatedWrites(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		})).ServeHTTP(recorder, request)
		if called == private {
			t.Errorf("%s: called = %t, want %t", path, called, !private)
		}
		if private && recorder.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", path, recorder.Code)
		}
	}

	// The user's own session still sees everything.
	request := httptest.NewRequest(http.MethodGet, "/api/v1/apps/blog/env", nil)
	request = request.WithContext(ctxkeys.WithSession(request.Context(), &model.Session{ID: "session-1"}))
	if !privateDuringImpersonation(request) {
		t.Fatal("the env API is not marked private")
	}
	called := false
	BlockImpersonatedWrites(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(httptest.NewRecorder(), request)
	if !called {
		t.Fatal("an ordinary session cannot read its env")
	}
}
