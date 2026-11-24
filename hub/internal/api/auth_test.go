package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/testutil"
)

func seedVerifiedUser(t *testing.T, environment *apiEnvironment, id, name string) *model.User {
	t.Helper()
	return testutil.SeedUser(t, environment.database, testutil.UserOptions{
		ID:       id,
		Email:    id + "@example.com",
		Name:     name,
		Verified: true,
	}).User
}

// TestEmailChangeVerify: like the link on the web, verifying the new
// address ends every session; a cookie caller gets a new one.
func TestEmailChangeVerify(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := seedVerifiedUser(t, environment, "mover", "Mover")
	if err := environment.auth.RequestEmailChange(user.ID, "moved@example.com"); err != nil {
		t.Fatalf("RequestEmailChange() error = %v", err)
	}
	token := environment.mailedToken(t, "/auth/verify-email-change/")
	other := environment.signIn(t, user.ID)
	caller := environment.signIn(t, user.ID)

	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/email-change/verify", body: `{"token":"` + token + `"}`, cookie: caller})
	expectStatus(t, recorder, http.StatusOK)
	response := decodeBody[api.UserResponse](t, recorder)
	if response.User.ID != user.ID || response.User.Email != "moved@example.com" {
		t.Fatalf("response = %#v", response)
	}
	fresh := cookieValue(recorder, "session_token")
	if fresh == "" || fresh == caller {
		t.Fatalf("the cookie caller got no new session: %v", recorder.Result().Cookies())
	}
	for _, old := range []string{other, caller} {
		stale := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: old})
		expectError(t, stale, http.StatusUnauthorized, "unauthenticated")
	}
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: fresh}), http.StatusOK)

	reused := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/email-change/verify", body: `{"token":"` + token + `"}`})
	expectError(t, reused, http.StatusBadRequest, "invalid_token")
}

// TestEmailChangeVerifySignsNobodyIn: the token is no way in. A caller
// signed in as another account gets no session, and keeps its own.
func TestEmailChangeVerifySignsNobodyIn(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := seedVerifiedUser(t, environment, "victim", "Victim")
	other := seedVerifiedUser(t, environment, "caller", "Caller")
	if err := environment.auth.RequestEmailChange(user.ID, "moved@example.com"); err != nil {
		t.Fatalf("RequestEmailChange() error = %v", err)
	}
	token := environment.mailedToken(t, "/auth/verify-email-change/")
	caller := environment.signIn(t, other.ID)

	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/email-change/verify", body: `{"token":"` + token + `"}`, cookie: caller})
	expectStatus(t, recorder, http.StatusOK)
	if cookieValue(recorder, "session_token") != "" {
		t.Fatalf("the caller got a session: %v", recorder.Result().Cookies())
	}
	mine := decodeBody[api.UserResponse](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: caller}))
	if mine.User.ID != other.ID {
		t.Fatalf("the caller is %q, want its own account", mine.User.ID)
	}
}

func TestOnboardingGate(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "fresh", Email: "fresh@example.com", Verified: true}).User
	token := environment.signIn(t, user.ID)

	account := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: token})
	expectStatus(t, account, http.StatusOK)
	if !decodeBody[struct {
		OnboardingRequired bool `json:"onboarding_required"`
	}](t, account).OnboardingRequired {
		t.Fatalf("onboarding_required is false: %s", account.Body.String())
	}

	gated := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/sessions", cookie: token})
	expectError(t, gated, http.StatusForbidden, "onboarding_required")

	invalid := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/onboarding", cookie: token, body: `{"name":"   "}`})
	detail := expectError(t, invalid, http.StatusUnprocessableEntity, "validation_failed")
	if detail.Fields["name"] == "" {
		t.Fatalf("fields = %#v", detail.Fields)
	}

	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/onboarding", cookie: token, body: `{"name":" Grace Hopper "}`})
	expectStatus(t, recorder, http.StatusOK)
	response := decodeBody[struct {
		User      api.User       `json:"user"`
		Workspace *api.Workspace `json:"workspace"`
	}](t, recorder)
	if response.User.Name != "Grace Hopper" {
		t.Fatalf("user = %#v", response.User)
	}
	if response.Workspace == nil || response.Workspace.Slug == "" {
		t.Fatalf("workspace = %#v, want the default workspace", response.Workspace)
	}

	open := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/sessions", cookie: token})
	expectStatus(t, open, http.StatusOK)
}

func TestOnboardingNamesTheWorkspace(t *testing.T) {
	environment := newAPIEnvironment(t)
	for _, test := range []struct {
		id, body, want string
	}{
		{id: "named", body: `{"name":"Grace Hopper","workspace_name":" Acme Labs "}`, want: "Acme Labs"},
		{id: "default", body: `{"name":"Grace Hopper"}`, want: "Grace Hopper's Workspace"},
	} {
		seeded := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: test.id, Email: test.id + "@example.com", Verified: true})
		// A new account has no workspace before onboarding.
		if _, err := environment.database.Exec(`DELETE FROM workspaces WHERE id = $1`, seeded.Workspace.ID); err != nil {
			t.Fatalf("remove fixture workspace: %v", err)
		}
		token := environment.signIn(t, seeded.User.ID)
		if test.id == "named" {
			invalid := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/onboarding", cookie: token, body: `{"name":"Grace Hopper","workspace_name":"x"}`})
			detail := expectError(t, invalid, http.StatusUnprocessableEntity, "validation_failed")
			if detail.Fields["workspace_name"] == "" {
				t.Fatalf("fields = %#v", detail.Fields)
			}
		}
		recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/onboarding", cookie: token, body: test.body})
		expectStatus(t, recorder, http.StatusOK)
		response := decodeBody[struct {
			Workspace *api.Workspace `json:"workspace"`
		}](t, recorder)
		if response.Workspace == nil || response.Workspace.Name != test.want {
			t.Fatalf("%s: workspace = %#v, want %q", test.id, response.Workspace, test.want)
		}
	}
}

func TestOnboardingRequiresSession(t *testing.T) {
	environment := newAPIEnvironment(t)
	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/onboarding", body: `{"name":"Nobody"}`})
	expectError(t, recorder, http.StatusUnauthorized, "unauthenticated")
}

func TestLogoutRevokesCookieSession(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := seedVerifiedUser(t, environment, "cookie", "Cookie")
	token := environment.signIn(t, user.ID)

	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/logout", cookie: token})
	expectStatus(t, recorder, http.StatusNoContent)
	cleared := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "session_token" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("session cookie was not cleared: %#v", recorder.Result().Cookies())
	}
	_, err := environment.auth.AuthenticateSession(token)
	if err == nil {
		t.Fatal("logout kept the session row")
	}
}

func TestCookieRequestWithoutCSRFHeaderIsRejected(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := seedVerifiedUser(t, environment, "csrf", "CSRF")
	token := environment.signIn(t, user.ID)

	request := apiRequest{method: http.MethodPost, path: "/api/v1/auth/logout"}
	recorder := environment.sendWithCookieOnly(t, request, token)
	detail := expectError(t, recorder, http.StatusForbidden, "forbidden")
	if detail.Message != "Send the X-CSRF-Token header with cookie requests." {
		t.Fatalf("message = %q", detail.Message)
	}
}

func TestLogoutRequiresSession(t *testing.T) {
	environment := newAPIEnvironment(t)
	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/logout", bearer: "unknown"})
	expectError(t, recorder, http.StatusUnauthorized, "unauthenticated")
}

func TestUnknownAPIPathIsJSON(t *testing.T) {
	environment := newAPIEnvironment(t)
	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/nothing"})
	expectError(t, recorder, http.StatusNotFound, "not_found")
}

// cookieValue returns the value of the named Set-Cookie header, or "".
func cookieValue(recorder *httptest.ResponseRecorder, name string) string {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}
