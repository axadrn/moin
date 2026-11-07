package handler

import (
	"bytes"
	"context"
	"errors"
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

// The typed name of the confirm dialog comes as X-Moin-Confirm, and the
// service checks it: removing an app, a bucket or a server and deleting the
// workspace are refused without it or with another name, also when the
// browser's check is skipped, and go with it (DESIGN.md, Asking first).
func TestTierOneNeedsTheTypedName(t *testing.T) {
	database := testutil.NewDatabase(t)
	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Name: "Ada", Verified: true})
	// A name a header cannot carry as it is: the dialog URI-encodes it.
	ada.Workspace.Name = "Müller & Söhne"
	if _, err := database.Exec(`UPDATE workspaces SET name = $1 WHERE id = $2`, ada.Workspace.Name, ada.Workspace.ID); err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	servers := service.NewServerService(repository.NewServers(database), repository.NewActivity(database), "https://moin.example.com")
	apps := service.NewAppService(repository.NewApps(database, database), servers, box)
	actor := service.Actor{WorkspaceID: ada.Workspace.ID, UserID: "ada"}
	server, _, err := servers.Create(actor, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(actor, "blog", server.ID); err != nil {
		t.Fatal(err)
	}
	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer store.Close()
	if _, err := apps.AddBucket(context.Background(), actor, service.BucketInput{Name: "spare", Endpoint: store.URL, Bucket: "spare", AccessKey: "k", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	workspaces := service.NewWorkspaceService(
		repository.NewWorkspaces(database),
		repository.NewWorkspaceMembers(database),
		repository.NewWorkspaceInvitations(database),
		repository.NewActivity(database),
		repository.NewUsers(database),
		service.NewSubscriptionService(repository.NewSubscriptions(database), repository.NewAdmin(database)),
		service.NewEmailService(testutil.NewMemoryMailer(), "https://app.example.com", "Test App"),
	)

	appsHandler := NewAppsHandler(apps, servers, nil, nil)
	base := "/app/" + ada.Workspace.Slug
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /app/{workspaceSlug}/apps/{app}", appsHandler.Delete)
	mux.HandleFunc("POST /app/{workspaceSlug}/buckets/{name}/remove", appsHandler.RemoveBucket)
	mux.HandleFunc("DELETE /app/{workspaceSlug}/servers/{id}", NewServersHandler(servers, nil).Delete)
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/delete", NewWorkspaceHandler(workspaces).Delete)
	send := func(method, path, typed string) string {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		request.Header.Set("HX-Request", "true")
		if typed != "" {
			request.Header.Set("X-Moin-Confirm", typed)
		}
		ctx := context.Background()
		ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{SupportEmail: "support@example.com"})
		ctx = ctxkeys.WithURLPath(ctx, path)
		ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
		ctx = ctxkeys.WithUser(ctx, ada.User)
		ctx = ctxkeys.WithWorkspace(ctx, ada.Workspace)
		ctx = ctxkeys.WithWorkspaceMember(ctx, &model.WorkspaceMember{WorkspaceID: ada.Workspace.ID, UserID: "ada", Role: model.WorkspaceRoleOwner})
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request.WithContext(ctx))
		return recorder.Header().Get("HX-Trigger") + recorder.Body.String()
	}

	// The app goes first: the bucket is the default every app uses.
	for _, thing := range []struct{ method, path, name string }{
		{"DELETE", base + "/apps/blog", "blog"},
		{"POST", base + "/buckets/spare/remove", "spare"},
		{"DELETE", base + "/servers/" + server.ID, "web-1"},
		{"POST", base + "/workspace/delete", url.PathEscape(ada.Workspace.Name)},
	} {
		for _, typed := range []string{"", "shop", "%zz"} {
			if page := send(thing.method, thing.path, typed); !strings.Contains(page, "Type the name to confirm.") {
				t.Errorf("%s %s with %q went through: %s", thing.method, thing.path, typed, page)
			}
		}
		if page := send(thing.method, thing.path, thing.name); strings.Contains(page, "Type the name to confirm.") || strings.Contains(page, "not removed") || strings.Contains(page, "not deleted") {
			t.Errorf("%s %s with the name: %s", thing.method, thing.path, page)
		}
	}
	if _, err := apps.Get(ada.Workspace.ID, "blog"); !errors.Is(err, repository.ErrAppNotFound) {
		t.Errorf("blog after removal: %v", err)
	}
	if _, err := apps.Bucket(ada.Workspace.ID, "spare"); !errors.Is(err, repository.ErrBucketNotFound) {
		t.Errorf("spare after removal: %v", err)
	}
	if _, err := servers.Get(ada.Workspace.ID, server.ID); !errors.Is(err, repository.ErrServerNotFound) {
		t.Errorf("web-1 after removal: %v", err)
	}
	if _, err := repository.NewWorkspaces(database).ByID(ada.Workspace.ID); !errors.Is(err, repository.ErrWorkspaceNotFound) {
		t.Errorf("the workspace after deletion: %v", err)
	}
}
