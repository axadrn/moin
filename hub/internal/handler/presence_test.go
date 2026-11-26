package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
)

// Confirm it's you with GitHub: the trip goes to GitHub and back to the
// page it came from. With the GitHub account the user signs in with, the
// session counts as confirmed now; it stays, and so does every other one.
// Another GitHub account confirms nothing.
func TestConfirmWithGitHubKeepsTheSession(t *testing.T) {
	g := newGitHubSignInTest(t, false)
	if err := g.auth.ConnectGitHub(g.owner.User.ID, service.GitHubAccount{ID: 5, Login: "owner"}); err != nil {
		t.Fatal(err)
	}
	owner, err := g.users.ByID(g.owner.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessions := repository.NewSessions(g.database)
	session, _, err := g.auth.CreateSession(httptest.NewRequest(http.MethodGet, "/", nil), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := g.auth.CreateSession(httptest.NewRequest(http.MethodGet, "/", nil), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	g.database.MustExec(`UPDATE sessions SET confirmed_at = $1 WHERE id = $2`, time.Now().Add(-time.Hour).UTC(), session.ID)
	session, err = sessions.ByTokenHash(session.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	next := "/device?code=ABCD-EFGH"

	start := func() *http.Cookie {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/auth/confirm/github", strings.NewReader(url.Values{"next": {next}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := ctxkeys.WithUser(request.Context(), owner)
		ctx = ctxkeys.WithSession(ctx, session)
		recorder := httptest.NewRecorder()
		g.handler.ConfirmWithGitHub(recorder, request.WithContext(ctx))
		location, _ := url.Parse(recorder.Header().Get("Location"))
		if recorder.Code != http.StatusSeeOther || location.Query().Get("redirect_uri") != "https://moin.example.com/auth/github/callback" {
			t.Fatalf("start = %d %q", recorder.Code, location)
		}
		return recorder.Result().Cookies()[0]
	}
	back := func(code string) *httptest.ResponseRecorder {
		t.Helper()
		state := start()
		value, _, _ := strings.Cut(state.Value, ".")
		request := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code="+code+"&state="+value, nil)
		request.AddCookie(state)
		ctx := ctxkeys.WithRenderConfig(request.Context(), githubAppConfig.Render())
		ctx = ctxkeys.WithUser(ctx, owner)
		ctx = ctxkeys.WithSession(ctx, session)
		recorder := httptest.NewRecorder()
		g.handler.GitHubCallback(recorder, request.WithContext(ctx))
		return recorder
	}
	confirmed := func() bool {
		t.Helper()
		stored, err := sessions.ByTokenHash(session.TokenHash)
		if err != nil {
			t.Fatalf("the session ended: %v", err)
		}
		return time.Since(stored.ConfirmedAt) < service.ConfirmedFor
	}

	if refused := back("stranger"); refused.Code != http.StatusForbidden || !strings.Contains(refused.Body.String(), "not the GitHub account you sign in with") || confirmed() {
		t.Fatalf("another GitHub account: %d %s", refused.Code, refused.Body.String())
	}
	done := back("owner")
	if done.Code != http.StatusSeeOther || done.Header().Get("Location") != next || !confirmed() {
		t.Fatalf("confirm = %d %q", done.Code, done.Header().Get("Location"))
	}
	for _, cookie := range done.Result().Cookies() {
		if cookie.Name == "session_token" {
			t.Fatalf("the confirmation touched the session cookie: %+v", cookie)
		}
	}
	if _, err := sessions.ByTokenHash(other.TokenHash); err != nil {
		t.Fatalf("the other session ended: %v", err)
	}
}

// A way back is a page of this hub, never another site.
func TestLocalPath(t *testing.T) {
	for next, want := range map[string]string{
		"/device?code=ABCD-EFGH":                     "/device?code=ABCD-EFGH",
		"/app/acme/settings/account?tab=security":    "/app/acme/settings/account?tab=security",
		"/oauth/authorize?client_id=1&state=x":       "/oauth/authorize?client_id=1&state=x",
		"":                                           "/app",
		"https://evil.example.com/":                  "/app",
		"//evil.example.com/":                        "/app",
		"/\\evil.example.com":                        "/app",
		"\\\\evil.example.com":                       "/app",
		"javascript:alert(1)":                        "/app",
		"app/acme":                                   "/app",
		"/app\r\nLocation: https://evil.example.com": "/app",
	} {
		if got := localPath(next); got != want {
			t.Errorf("localPath(%q) = %q, want %q", next, got, want)
		}
	}
}

// Confirming with a passkey that does not work leads back all the same,
// and only to a page of this hub; the page there asks again.
func TestConfirmWithPasskeyGoesBackHome(t *testing.T) {
	test := newDeviceHandlerTest(t, true)
	handler := NewAuthHandler(test.auth, test.workspaces, nil, test.passkeys, &githubAppConfig)
	session := test.session(t, time.Hour)
	test.db.MustExec(`UPDATE sessions SET confirmed_at = $1 WHERE id = $2`, session.ConfirmedAt.UTC(), session.ID)
	for next, want := range map[string]string{"/device?code=ABCD-EFGH": "/device?code=ABCD-EFGH", "https://evil.example.com": "/app"} {
		recorder := httptest.NewRecorder()
		handler.ConfirmWithPasskey(recorder, deviceRequest(http.MethodPost, "/auth/confirm/passkey", url.Values{"next": {next}, "passkey": {"{}"}}, test.owner.User, session))
		if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != want {
			t.Fatalf("next %q: %d %q", next, recorder.Code, recorder.Header().Get("Location"))
		}
	}
	stored, err := repository.NewSessions(test.db).ByTokenHash(session.TokenHash)
	if err != nil || test.passkeys.Present(stored) == nil {
		t.Fatalf("a passkey that did not work confirmed the session: %+v, %v", stored, err)
	}
}
