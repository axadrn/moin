package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/internal/link"
)

// request runs one request to the app pages of s with the workspace in
// the context, as the middleware puts it.
func (s *switches) request(method, pattern, path string, form url.Values, run http.HandlerFunc) *httptest.ResponseRecorder {
	s.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	user, err := repository.NewUsers(s.database).ByID(s.ada.User.ID)
	if err != nil {
		s.t.Fatal(err)
	}
	ctx := context.Background()
	ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{SupportEmail: "support@example.com"})
	ctx = ctxkeys.WithURLPath(ctx, path)
	ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
	ctx = ctxkeys.WithUser(ctx, user)
	ctx = ctxkeys.WithWorkspace(ctx, s.ada.Workspace)
	ctx = ctxkeys.WithWorkspaceMember(ctx, &model.WorkspaceMember{WorkspaceID: s.ada.Workspace.ID, UserID: "ada", Role: model.WorkspaceRoleOwner})
	mux := http.NewServeMux()
	mux.HandleFunc(method+" "+pattern, run)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request.WithContext(ctx))
	return recorder
}

// TestRenameAppInTheWeb: Settings renames the app and shows it under its
// new link; the page under a link with the old name, or any name, still
// opens, and a link with an unknown ID or a bad one does not.
func TestRenameAppInTheWeb(t *testing.T) {
	s := newSwitches(t)
	app := s.app()
	base := s.ada.Workspace.Path()
	answer := s.request("POST", "/app/{workspaceSlug}/apps/{app}/name", base+"/apps/"+app.Link()+"/name", url.Values{"name": {"journal"}}, s.handler.RenameApp)
	renamed := model.App{ID: app.ID, Name: "journal"}
	if answer.Code != http.StatusOK || answer.Header().Get("HX-Replace-Url") != base+"/apps/"+renamed.Link()+"/settings" {
		t.Fatalf("rename = %d, url %q: %s", answer.Code, answer.Header().Get("HX-Replace-Url"), answer.Body.String())
	}
	// The head names the address asked for; the page links the new one.
	_, body, _ := strings.Cut(answer.Body.String(), "</head>")
	if !strings.Contains(body, "blog renamed") || !strings.Contains(body, `value="journal"`) || !strings.Contains(body, "/apps/"+renamed.Link()+"/settings") || strings.Contains(body, "/apps/"+app.Link()) {
		t.Fatalf("the settings after the rename: %s", body)
	}
	if activity(t, s, "app.renamed") != 1 {
		t.Fatal("no app.renamed in the activity")
	}
	if got, err := s.apps.Get(s.ada.Workspace.ID, "journal"); err != nil || got.Folder != "blog" {
		t.Fatalf("renamed app = %+v, %v", got, err)
	}

	bad := s.request("POST", "/app/{workspaceSlug}/apps/{app}/name", base+"/apps/"+renamed.Link()+"/name", url.Values{"name": {"Not Valid"}}, s.handler.RenameApp)
	if trigger := bad.Header().Get("HX-Trigger"); !strings.Contains(trigger, "app-rename") || !strings.Contains(trigger, "lowercase letters") {
		t.Fatalf("a bad name: %s", trigger)
	}

	open := func(ref string) int {
		return s.request("GET", "/app/{workspaceSlug}/apps/{app}/settings", base+"/apps/"+ref+"/settings", nil, s.handler.AppSection("settings")).Code
	}
	for _, ref := range []string{renamed.Link(), "journal", link.ID(app.ID)} {
		if code := open(ref); code != http.StatusOK {
			t.Errorf("%s = %d, want the page", ref, code)
		}
	}
	for _, ref := range []string{"blog", "journal-" + link.ID(app.ID), "journal-12345"} {
		if code := open(ref); code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", ref, code)
		}
	}
}
