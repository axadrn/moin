package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// TestGitHubAppCreatedTakesTheAdminBack: GitHub's return link creates the
// hub's app for an admin and goes into the app; anyone else gets why not,
// and the hub stays without one.
func TestGitHubAppCreatedTakesTheAdminBack(t *testing.T) {
	database, workspaces, _, _ := invitationHandlerTestSetup(t)
	admin := testutil.SeedUser(t, database, testutil.UserOptions{ID: "hub-admin", Email: "admin@example.com", Verified: true})
	member := testutil.SeedUser(t, database, testutil.UserOptions{ID: "hub-member", Verified: true})
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app-manifests/good/conversions" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "slug": "moin-hub", "client_id": "Iv23.hub", "client_secret": "s", "webhook_secret": "w", "pem": githubKeyForTests(t)})
	}))
	t.Cleanup(github.Close)
	box, err := secret.New(bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	githubApp, err := service.NewGitHubAppService(service.GitHubAppSettings{}, repository.NewGitHubApps(database), box, repository.NewActivity(database), service.NewHubAdmins([]string{"admin@example.com"}), "https://moin.example.com", github.URL, github.URL)
	if err != nil {
		t.Fatal(err)
	}
	action, _ := url.Parse(githubApp.Manifest(admin.User).Action)
	state := action.Query().Get("state")
	handler := NewGitHubAppHandler(githubApp, workspaces)
	back := func(user *model.User) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/app/github/created?code=good&state="+url.QueryEscape(state), nil)
		ctx := ctxkeys.WithUser(ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{}), user)
		recorder := httptest.NewRecorder()
		handler.Created(recorder, request.WithContext(ctx))
		return recorder
	}

	if response := back(member.User); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Only the admins of this hub") || githubApp.GitHub() != nil {
		t.Fatalf("a member: %d %s", response.Code, response.Body.String())
	}
	if response := back(admin.User); response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/app" {
		t.Fatalf("the admin: %d %q %s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	if !githubApp.SignsIn() {
		t.Fatal("the hub does not sign in with its new app")
	}
	events, err := repository.NewActivity(database).ListByWorkspace(admin.Workspace.ID, 5)
	if err != nil || len(events) == 0 || events[0].Action != "hub.github_app_created" {
		t.Fatalf("activity = %+v, %v", events, err)
	}
	if response := back(admin.User); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "has its GitHub App already") {
		t.Fatalf("again: %d %s", response.Code, response.Body.String())
	}
}
