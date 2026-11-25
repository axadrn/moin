package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// TestGitHubAppPutsTheFormOnlyForAnAdmin: pages of a hub without a GitHub
// App get the form that creates it for an admin, never for anyone else,
// for an impersonation or for a change, and nobody signs in with GitHub.
func TestGitHubAppPutsTheFormOnlyForAnAdmin(t *testing.T) {
	database := testutil.NewDatabase(t)
	box, err := secret.New(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	githubApp, err := service.NewGitHubAppService(service.GitHubAppSettings{}, repository.NewGitHubApps(database), box, nil, service.NewHubAdmins([]string{"admin@example.com"}), "https://hub.example.com", "", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	admin := &model.User{ID: "admin", Email: "admin@example.com", EmailVerifiedAt: &now}
	member := &model.User{ID: "member", Email: "member@example.com", EmailVerifiedAt: &now}
	operator := "operator"
	for _, test := range []struct {
		name    string
		method  string
		user    *model.User
		session *model.Session
		form    bool
	}{
		{name: "admin", method: http.MethodGet, user: admin, form: true},
		{name: "member", method: http.MethodGet, user: member},
		{name: "guest", method: http.MethodGet},
		{name: "a change", method: http.MethodPost, user: admin},
		{name: "impersonated admin", method: http.MethodGet, user: admin, session: &model.Session{ImpersonatorUserID: &operator}},
	} {
		var form *model.GitHubAppManifest
		var render config.RenderConfig
		handler := GitHubApp(githubApp)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			form, render = ctxkeys.GitHubAppManifest(r.Context()), ctxkeys.RenderConfig(r.Context())
		}))
		request := httptest.NewRequest(test.method, "/app/acme/overview", nil)
		ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{GitHubLogin: true, SupportEmail: "help@example.com"})
		ctx = ctxkeys.WithSession(ctxkeys.WithUser(ctx, test.user), test.session)
		handler.ServeHTTP(httptest.NewRecorder(), request.WithContext(ctx))
		if (form != nil) != test.form {
			t.Errorf("%s: form = %v, want %v", test.name, form != nil, test.form)
		}
		if render.GitHubLogin || render.SupportEmail != "help@example.com" {
			t.Errorf("%s: render = %+v, want no GitHub sign-in and the rest kept", test.name, render)
		}
	}
}
