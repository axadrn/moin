package handler

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
)

// TestGitHubAuthNeedsClient: without a client ID and secret, GitHub would
// show an error page, so the sign-in stays on moin.
func TestGitHubAuthNeedsClient(t *testing.T) {
	handler := &authHandler{cfg: &config.Config{APIURL: "https://api.example.com"}}
	response := httptest.NewRecorder()
	handler.GitHubAuth(response, httptest.NewRequest(http.MethodGet, "/auth/github", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/auth" {
		t.Fatalf("GitHubAuth() without a client = %d %q, want 303 to /auth", response.Code, response.Header().Get("Location"))
	}
	if !(&config.Config{GitHubClientID: "client", GitHubClientSecret: "secret"}).Render().GitHubLogin || (&config.Config{GitHubClientID: "client"}).Render().GitHubLogin {
		t.Fatal("GitHubLogin must need both the client ID and the secret")
	}
}

func TestGitHubAuthUsesOneTimeStateCookie(t *testing.T) {
	handler := &authHandler{cfg: &config.Config{AppURL: "https://app.example.com", APIURL: "https://api.example.com", GitHubClientID: "client", GitHubClientSecret: "secret"}}
	request := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	request = request.WithContext(ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{AppEnv: "production"}))
	response := httptest.NewRecorder()

	handler.GitHubAuth(response, request)
	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	cookies := response.Result().Cookies()
	if response.Code != http.StatusTemporaryRedirect || len(cookies) != 1 {
		t.Fatalf("GitHubAuth() status = %d, cookies = %#v", response.Code, cookies)
	}
	cookie := cookies[0]
	if cookie.Name != "oauth_state" || cookie.Value == "" || cookie.Value != location.Query().Get("state") {
		t.Fatalf("OAuth state cookie = %#v, redirect = %q", cookie, location.String())
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 600 {
		t.Fatalf("OAuth state cookie flags = %#v", cookie)
	}
	redirectURI := location.Query().Get("redirect_uri")
	if redirectURI != "https://api.example.com/auth/github/callback" {
		t.Fatalf("redirect_uri = %q, want the API_URL callback", redirectURI)
	}
}

// TestGitHubCallbackWithStaleStateWhenSignedIn: a sign-in that comes back
// after another one finished goes into the app, not to the sign-in page.
func TestGitHubCallbackWithStaleStateWhenSignedIn(t *testing.T) {
	handler := &authHandler{cfg: &config.Config{}}
	request := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=c&state=old", nil)
	request = request.WithContext(ctxkeys.WithUser(request.Context(), &model.User{ID: "ada"}))
	response := httptest.NewRecorder()

	handler.GitHubCallback(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/app" {
		t.Fatalf("GitHubCallback() = %d %q, want 303 to /app", response.Code, response.Header().Get("Location"))
	}
}

// TestGitHubCallbackFailureLeavesTheEmailFieldAlone: a failed GitHub sign-in
// says so on its own and does not mark the email field.
func TestGitHubCallbackFailureLeavesTheEmailFieldAlone(t *testing.T) {
	handler := &authHandler{cfg: &config.Config{}}
	request := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=c&state=old", nil)
	request = request.WithContext(ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{GitHubLogin: true}))
	response := httptest.NewRecorder()

	handler.GitHubCallback(response, request)

	body := response.Body.String()
	if strings.Contains(body, `aria-invalid="true"`) || !strings.Contains(body, `role="alert"`) {
		t.Fatalf("GitHubCallback() body marks the email field or has no alert:\n%s", body)
	}
}

// fakeGitHubAPI answers GitHub API requests with the JSON body for each URL.
type fakeGitHubAPI map[string]string

func (f fakeGitHubAPI) RoundTrip(request *http.Request) (*http.Response, error) {
	body, ok := f[request.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
		body = `{"message":"Not Found"}`
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {"application/json"}}, Request: request}, nil
}

func TestGitHubEmailRequiresAVerifiedAddress(t *testing.T) {
	for _, test := range []struct {
		name    string
		user    string
		emails  string
		want    []string
		wantErr error
	}{
		{name: "verified public email", user: `{"email":"Public@Example.com"}`, emails: `[{"email":"primary@example.com","primary":true,"verified":true},{"email":"public@example.com","verified":true}]`, want: []string{"public@example.com", "primary@example.com"}},
		{name: "verified primary when private", user: `{"email":null}`, emails: `[{"email":"primary@example.com","primary":true,"verified":true}]`, want: []string{"primary@example.com"}},
		{name: "unverified public falls back to verified primary", user: `{"email":"victim@example.com"}`, emails: `[{"email":"victim@example.com","verified":false},{"email":"primary@example.com","primary":true,"verified":true}]`, want: []string{"primary@example.com"}},
		{name: "every verified address, primary first", user: `{"email":null}`, emails: `[{"email":"work@example.com","verified":true},{"email":"unproven@example.com","verified":false},{"email":"primary@example.com","primary":true,"verified":true}]`, want: []string{"primary@example.com", "work@example.com"}},
		{name: "unverified primary", user: `{"email":null}`, emails: `[{"email":"victim@example.com","primary":true,"verified":false}]`, wantErr: errOAuthEmailUnverified},
		{name: "unverified public only", user: `{"email":"victim@example.com"}`, emails: `[{"email":"victim@example.com","primary":true,"verified":false}]`, wantErr: errOAuthEmailUnverified},
		{name: "no email", user: `{"email":null}`, emails: `[]`, wantErr: errOAuthEmailMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: fakeGitHubAPI{
				"https://api.github.com/user":        test.user,
				"https://api.github.com/user/emails": test.emails,
			}}

			emails, _, err := githubAccount(client, "https://api.github.com")

			if !errors.Is(err, test.wantErr) || !slices.Equal(emails, test.want) {
				t.Fatalf("githubAccount() = %q, %v, want %q, %v", emails, err, test.want, test.wantErr)
			}
		})
	}
}

func TestGitHubEmailFailsWhenTheEmailListIsUnavailable(t *testing.T) {
	client := &http.Client{Transport: fakeGitHubAPI{"https://api.github.com/user": `{"email":"public@example.com"}`}}

	emails, _, err := githubAccount(client, "https://api.github.com")

	if err == nil || emails != nil {
		t.Fatalf("githubAccount() = %q, %v, want an error", emails, err)
	}
}
