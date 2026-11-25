package handler

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/jmoiron/sqlx"
)

// githubAppConfig is a hub with the GitHub App set up.
var githubAppConfig = config.Config{AppURL: "https://moin.example.com", GitHubAppID: "1", GitHubAppClientID: "Iv1.test", GitHubAppClientSecret: "secret"}

var (
	githubKeyOnce sync.Once
	githubKey     string
)

// fakeGitHub signs in the account whose login is the code: the code
// "stranger" is the GitHub account stranger with the verified primary email
// stranger@example.com.
func fakeGitHub(t *testing.T, ids map[string]int64) *service.GitHubAppService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ghu_")
		switch r.URL.Path {
		case "/login/oauth/access_token":
			var grant map[string]string
			_ = json.NewDecoder(r.Body).Decode(&grant)
			if grant["redirect_uri"] != "https://moin.example.com/auth/github/callback" {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "redirect_uri_mismatch"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "ghu_" + grant["code"]})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": ids[login], "login": login, "name": nil})
		case "/user/emails":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"email": login + "@example.com", "primary": true, "verified": true}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	github, err := service.NewGitHubAppService(service.GitHubAppSettings{ID: "1", Slug: "moin-test", PrivateKey: githubKeyForTests(t), ClientID: "Iv1.test", ClientSecret: "secret"}, nil, nil, nil, nil, "https://moin.example.com", server.URL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return github
}

// githubKeyForTests is a private key of a GitHub App, made once.
func githubKeyForTests(t *testing.T) string {
	t.Helper()
	githubKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		githubKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	})
	return githubKey
}

type githubSignInTest struct {
	database   *sqlx.DB
	users      *repository.Users
	auth       *service.AuthService
	workspaces *service.WorkspaceService
	owner      testutil.UserFixture
	mailer     *testutil.MemoryMailer
	handler    *authHandler
}

func newGitHubSignInTest(t *testing.T, open bool) githubSignInTest {
	t.Helper()
	database, workspaces, owner, mailer := invitationHandlerTestSetup(t)
	users := repository.NewUsers(database)
	email := service.NewEmailService(mailer, "https://moin.example.com", "Test App")
	auth := service.NewAuthService(users, repository.NewTokens(database), repository.NewSessions(database), workspaces, email, false, time.Hour, time.Hour, time.Hour)
	auth.SetBeta(service.NewBetaService(open, []string{"admin@example.com"}, repository.NewWorkspaceInvitations(database), repository.NewSubscriptions(database)))
	cfg := githubAppConfig
	github := fakeGitHub(t, map[string]int64{"stranger": 1, "admin": 2, "mate": 3, "hopper": 4, "owner": 5})
	return githubSignInTest{database: database, users: users, auth: auth, workspaces: workspaces, owner: owner, mailer: mailer, handler: NewAuthHandler(auth, workspaces, github, nil, &cfg)}
}

// start goes to GitHub as user (nil for a guest) and returns the state
// cookie.
func (g githubSignInTest) start(t *testing.T, user *model.User) *http.Cookie {
	t.Helper()
	recorder := httptest.NewRecorder()
	status := http.StatusTemporaryRedirect
	if user == nil {
		g.handler.GitHubAuth(recorder, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	} else {
		request := httptest.NewRequest(http.MethodPost, "/auth/github/connect", nil)
		ctx := ctxkeys.WithUser(request.Context(), user)
		ctx = ctxkeys.WithSession(ctx, &model.Session{ID: "session", UserID: user.ID})
		g.handler.ConnectGitHub(recorder, request.WithContext(ctx))
		status = http.StatusSeeOther
	}
	location, _ := url.Parse(recorder.Header().Get("Location"))
	if recorder.Code != status || location.Query().Get("redirect_uri") != "https://moin.example.com/auth/github/callback" {
		t.Fatalf("start = %d %q", recorder.Code, location)
	}
	return recorder.Result().Cookies()[0]
}

// callback comes back from GitHub as the account named code.
func (g githubSignInTest) callback(t *testing.T, state *http.Cookie, code string, user *model.User, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	value, _, _ := strings.Cut(state.Value, ".")
	request := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code="+code+"&state="+value, nil)
	request.AddCookie(state)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	render := githubAppConfig.Render()
	render.GitHubLogin = true
	ctx := ctxkeys.WithRenderConfig(request.Context(), render)
	if user != nil {
		ctx = ctxkeys.WithUser(ctx, user)
	}
	recorder := httptest.NewRecorder()
	g.handler.GitHubCallback(recorder, request.WithContext(ctx))
	return recorder
}

// TestGitHubAuthNeedsTheApp: without the GitHub App with its client ID and
// secret, GitHub would show an error page, so the sign-in stays on moin.
func TestGitHubAuthNeedsTheApp(t *testing.T) {
	handler := &authHandler{cfg: &config.Config{GitHubAppID: "1"}}
	response := httptest.NewRecorder()
	handler.GitHubAuth(response, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth" {
		t.Fatalf("GitHubAuth() without the app = %d %q, want 303 to /auth", response.Code, response.Header().Get("Location"))
	}
}

func TestGitHubAuthUsesOneTimeStateCookie(t *testing.T) {
	cfg := githubAppConfig
	cfg.AppEnv = config.EnvironmentProduction
	handler := &authHandler{cfg: &cfg, githubApp: fakeGitHub(t, nil)}
	response := httptest.NewRecorder()

	handler.GitHubAuth(response, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	cookies := response.Result().Cookies()
	if response.Code != http.StatusTemporaryRedirect || len(cookies) != 1 {
		t.Fatalf("GitHubAuth() status = %d, cookies = %#v", response.Code, cookies)
	}
	cookie := cookies[0]
	if cookie.Name != "oauth_state" || cookie.Value != location.Query().Get("state")+"." || location.Query().Get("state") == "" {
		t.Fatalf("state cookie = %#v, redirect = %q", cookie, location.String())
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 600 {
		t.Fatalf("state cookie flags = %#v", cookie)
	}
	if redirectURI := location.Query().Get("redirect_uri"); redirectURI != "https://moin.example.com/auth/github/callback" {
		t.Fatalf("redirect_uri = %q, want the APP_URL callback", redirectURI)
	}
}

// TestGitHubCallbackWithStaleStateWhenSignedIn: a sign-in that comes back
// after another one finished goes into the app, not to the sign-in page.
func TestGitHubCallbackWithStaleStateWhenSignedIn(t *testing.T) {
	cfg := githubAppConfig
	handler := &authHandler{cfg: &cfg}
	request := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=c&state=old", nil)
	request = request.WithContext(ctxkeys.WithUser(request.Context(), &model.User{ID: "ada"}))
	response := httptest.NewRecorder()

	handler.GitHubCallback(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/app" {
		t.Fatalf("GitHubCallback() = %d %q, want 303 to /app", response.Code, response.Header().Get("Location"))
	}
}

func TestClosedSignupOnEachKindOfHub(t *testing.T) {
	for _, test := range []struct {
		name    string
		billing bool
		open    bool
		login   string
		private bool
	}{
		{name: "self-hosted refuses a stranger", login: "stranger", private: true},
		{name: "self-hosted lets the admin sign up", login: "admin"},
		{name: "self-hosted lets an invited teammate sign up", login: "mate"},
		{name: "self-hosted with SIGNUPS_OPEN=true is open", open: true, login: "stranger"},
		{name: "hosted with sign-up closed refuses a stranger", billing: true, login: "stranger", private: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := newGitHubSignInTest(t, test.open)
			if err := g.workspaces.Invite(g.owner.WorkspaceMember, g.owner.Workspace, "mate@example.com", model.WorkspaceRoleMember, service.Source{Via: "web"}); err != nil {
				t.Fatalf("Invite() error = %v", err)
			}

			recorder := g.callback(t, g.start(t, nil), test.login, nil)

			address := test.login + "@example.com"
			if test.private {
				if !strings.Contains(recorder.Body.String(), "is private") {
					t.Fatalf("status = %d, the page does not say it is private", recorder.Code)
				}
				if _, err := g.users.ByEmail(address); err == nil {
					t.Fatalf("%s got an account", address)
				}
				return
			}
			if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth/onboarding" {
				t.Fatalf("status = %d, location = %q, want onboarding", recorder.Code, recorder.Header().Get("Location"))
			}
			user, err := g.users.ByEmail(address)
			if err != nil || user.GitHubLogin != test.login {
				t.Fatalf("account of %s = %+v, %v", address, user, err)
			}
		})
	}
}

// TestGitHubCallbackTakesAnInvitationWithAnyAccount: the invitation link
// this browser opened makes the account, with the invited email, for a
// GitHub account of another address on a closed hub.
func TestGitHubCallbackTakesAnInvitationWithAnyAccount(t *testing.T) {
	g := newGitHubSignInTest(t, false)
	if err := g.workspaces.Invite(g.owner.WorkspaceMember, g.owner.Workspace, "grace@example.com", model.WorkspaceRoleMember, service.Source{Via: "web"}); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	token := invitationToken(t, g.mailer)

	recorder := g.callback(t, g.start(t, nil), "hopper", nil, &http.Cookie{Name: "pending_invitation", Value: token})

	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth/onboarding" {
		t.Fatalf("status = %d, location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
	user, err := g.users.ByEmail("grace@example.com")
	if err != nil || user.GitHubLogin != "hopper" || user.EmailVerifiedAt == nil {
		t.Fatalf("invited account = %+v, %v", user, err)
	}
}

// TestConnectGitHubFromSettings: an account without GitHub, after a
// recovery key, connects one, which nobody else signs in with.
func TestConnectGitHubFromSettings(t *testing.T) {
	g := newGitHubSignInTest(t, false)
	owner := g.owner.User
	taken := createHandlerTestUser(t, g.database, "taken", "taken@example.com", "Taken")
	if err := g.auth.ConnectGitHub(taken.ID, service.GitHubAccount{ID: 4, Login: "hopper"}); err != nil {
		t.Fatal(err)
	}

	refused := g.callback(t, g.start(t, owner), "hopper", owner)
	if refused.Code != http.StatusConflict || !strings.Contains(refused.Body.String(), "connected to another account") {
		t.Fatalf("a taken GitHub account: status = %d", refused.Code)
	}
	someoneElse := g.callback(t, g.start(t, owner), "owner", taken)
	if someoneElse.Code != http.StatusOK || !strings.Contains(someoneElse.Body.String(), "no longer valid") {
		t.Fatalf("another user finished the connect: status = %d", someoneElse.Code)
	}

	connected := g.callback(t, g.start(t, owner), "owner", owner)
	if connected.Code != http.StatusSeeOther || connected.Header().Get("Location") != g.owner.Workspace.Path()+"/settings/account?tab=security" {
		t.Fatalf("connect = %d %q", connected.Code, connected.Header().Get("Location"))
	}
	stored, _ := g.users.ByID(owner.ID)
	if stored.GitHubID == nil || *stored.GitHubID != 5 || stored.GitHubLogin != "owner" {
		t.Fatalf("stored = %+v", stored)
	}
}

// TestSignInWithKey: the key from the terminal signs in once, signs out
// every other session, disconnects GitHub and leads to the security
// settings, to connect GitHub again.
func TestSignInWithKey(t *testing.T) {
	g := newGitHubSignInTest(t, false)
	if err := g.auth.ConnectGitHub(g.owner.User.ID, service.GitHubAccount{ID: 5, Login: "owner"}); err != nil {
		t.Fatal(err)
	}
	_, other, err := g.auth.CreateSession(httptest.NewRequest(http.MethodGet, "/auth/github/callback", nil), g.owner.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := g.auth.CreateRecoveryKey("owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	send := func(key string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/auth/key", strings.NewReader(url.Values{"key": {key}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request = request.WithContext(ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{}))
		recorder := httptest.NewRecorder()
		g.handler.SignInWithKey(recorder, request)
		return recorder
	}

	recorder := send(key)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != g.owner.Workspace.Path()+"/settings/account?tab=security" {
		t.Fatalf("status = %d, location = %q, want the security settings", recorder.Code, recorder.Header().Get("Location"))
	}
	var session *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "session_token" {
			session = cookie
		}
	}
	if session == nil || session.Value == "" {
		t.Fatal("no session cookie")
	}
	if _, err := g.auth.AuthenticateSession(other); err == nil {
		t.Error("another session survived the recovery key")
	}
	if stored, _ := g.users.ByID(g.owner.User.ID); stored.GitHubID != nil || stored.GitHubLogin != "" {
		t.Errorf("GitHub is still connected: %+v", stored)
	}
	if again := send(key); !strings.Contains(again.Body.String(), "wrong, used or expired") || again.Header().Get("Set-Cookie") != "" {
		t.Fatalf("the used key: status = %d, body %s", again.Code, again.Body.String())
	}
}

// TestSignInPageHasOneWayIn: with the GitHub App only GitHub, never the key
// field; without it only the key field.
func TestSignInPageHasOneWayIn(t *testing.T) {
	for _, github := range []bool{true, false} {
		request := httptest.NewRequest(http.MethodGet, "/auth", nil)
		request = request.WithContext(ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{GitHubLogin: github}))
		recorder := httptest.NewRecorder()

		(&authHandler{}).AuthPage(recorder, request)

		body := recorder.Body.String()
		hasGitHub := strings.Contains(body, `href="/auth/github"`)
		hasKey := strings.Contains(body, `name="key"`) || strings.Contains(body, "/auth/key")
		if hasGitHub != github || hasKey == github {
			t.Errorf("GitHub App %v: GitHub button %v, key field %v", github, hasGitHub, hasKey)
		}
	}
}

// TestEmailChangeLinkSignsNobodyIn: the link changes the email and ends
// every session of the account. Only a browser signed in as that account
// stays in, with the session it had: the link never makes a session, so it
// cannot make an old one look freshly signed in. Anyone else, or another
// account, goes to sign in.
func TestEmailChangeLinkSignsNobodyIn(t *testing.T) {
	for _, test := range []struct {
		name     string
		as       string
		signedIn bool
	}{
		{name: "guest"},
		{name: "another account", as: "taken"},
		{name: "the same account", as: "handler-owner", signedIn: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := newGitHubSignInTest(t, false)
			createHandlerTestUser(t, g.database, "taken", "taken@example.com", "Taken")
			_, before, err := g.auth.CreateSession(httptest.NewRequest(http.MethodGet, "/", nil), g.owner.User.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := g.auth.RequestEmailChange(g.owner.User.ID, "moved@example.com"); err != nil {
				t.Fatal(err)
			}
			var token string
			for _, message := range g.mailer.Messages() {
				if match := regexp.MustCompile(`/auth/verify-email-change/([0-9a-f]{64})`).FindStringSubmatch(message.Text); match != nil {
					token = match[1]
				}
			}
			request := httptest.NewRequest(http.MethodGet, "/auth/verify-email-change/"+token, nil)
			request.SetPathValue("token", token)
			ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{})
			var browser string
			if test.as != "" {
				session, raw, err := g.auth.CreateSession(request, test.as)
				if err != nil {
					t.Fatal(err)
				}
				browser = raw
				request.AddCookie(&http.Cookie{Name: "session_token", Value: raw})
				user, _ := g.users.ByID(test.as)
				ctx = ctxkeys.WithSession(ctxkeys.WithUser(ctx, user), session)
			}
			recorder := httptest.NewRecorder()

			g.handler.VerifyEmailChange(recorder, request.WithContext(ctx))

			if stored, _ := g.users.ByID(g.owner.User.ID); stored.Email != "moved@example.com" {
				t.Fatalf("email = %q", stored.Email)
			}
			if _, err := g.auth.AuthenticateSession(before); err == nil {
				t.Error("a session of the account survived the email change")
			}
			newSession := ""
			for _, cookie := range recorder.Result().Cookies() {
				if cookie.Name == "session_token" && cookie.Value != "" {
					newSession = cookie.Value
				}
			}
			if !test.signedIn {
				if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/auth?email=changed" || newSession != "" {
					t.Fatalf("status = %d, location = %q, session %q, want sign in again", recorder.Code, recorder.Header().Get("Location"), newSession)
				}
				if browser != "" {
					if _, err := g.auth.AuthenticateSession(browser); err != nil {
						t.Errorf("the session of another account ended: %v", err)
					}
				}
				return
			}
			if recorder.Code != http.StatusOK || newSession != "" {
				t.Fatalf("status = %d, new session %q, want this browser to keep its session", recorder.Code, newSession)
			}
			if _, err := g.auth.AuthenticateSession(browser); err != nil {
				t.Errorf("the session of this browser ended: %v", err)
			}
		})
	}
}
