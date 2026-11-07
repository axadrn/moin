package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/secret"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

// TestRemoveNeedsTheName: removing an app, a bucket or a server answers
// 422 confirm_mismatch without ?confirm= or with another name, and goes
// with the name typed out (DESIGN.md, Asking first).
func TestRemoveNeedsTheName(t *testing.T) {
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "owner"})
	box, err := secret.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	servers := service.NewServerService(repository.NewServers(database), repository.NewActivity(database), "https://moin.example.com")
	apps := service.NewAppService(repository.NewApps(database, database), servers, box)
	actor := service.Actor{WorkspaceID: "workspace-owner", UserID: "owner"}
	server, _, err := servers.Create(actor, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(actor, "blog", server.ID); err != nil {
		t.Fatal(err)
	}
	bucket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer bucket.Close()
	if _, err := apps.AddBucket(context.Background(), actor, service.BucketInput{Name: "spare", Endpoint: bucket.URL, Bucket: "spare", AccessKey: "k", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}

	appsHandler := NewAppsHandler(apps)
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/apps/{app}", appsHandler.Delete)
	mux.HandleFunc("DELETE /api/v1/buckets/{name}", appsHandler.DeleteBucket)
	mux.HandleFunc("DELETE /api/v1/servers/{id}", NewServersHandler(servers).Delete)
	send := func(path string) (int, string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodDelete, path, nil)
		ctx := ctxkeys.WithWorkspace(ctxkeys.WithUser(request.Context(), &model.User{ID: "owner"}), &model.Workspace{ID: "workspace-owner"})
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request.WithContext(ctx))
		var answer ErrorBody
		_ = json.Unmarshal(recorder.Body.Bytes(), &answer)
		return recorder.Code, answer.Error.Code
	}

	// The app goes first: the bucket is the default every app uses.
	for _, thing := range []struct{ path, name string }{
		{"/api/v1/apps/blog", "blog"},
		{"/api/v1/buckets/spare", "spare"},
		{"/api/v1/servers/web-1", "web-1"},
	} {
		for _, query := range []string{"", "?confirm=", "?confirm=shop"} {
			if status, code := send(thing.path + query); status != http.StatusUnprocessableEntity || code != "confirm_mismatch" {
				t.Fatalf("DELETE %s%s = %d %q, want 422 confirm_mismatch", thing.path, query, status, code)
			}
		}
		if status, code := send(thing.path + "?confirm=" + thing.name); status >= 300 {
			t.Fatalf("DELETE %s with its name = %d %q", thing.path, status, code)
		}
	}
	if _, err := apps.Get(actor.WorkspaceID, "blog"); err == nil {
		t.Fatal("blog is still there")
	}
}
