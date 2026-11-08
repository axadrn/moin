package api_test

import (
	"net/http"
	"testing"

	"github.com/axadrn/moin/hub/internal/access"
	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// TestRenameAppAndNameOrID: PATCH /apps/{app} renames an app; answers
// carry IDs of 32 characters, and every path takes a name, an ID or a link
// part with any name, while the old name alone and an unknown ID are not
// found.
func TestRenameAppAndNameOrID(t *testing.T) {
	saved := access.Product
	access.Product = access.SelfHosted
	t.Cleanup(func() { access.Product = saved })
	environment := newAPIEnvironment(t)
	ada := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Name: "Ada", Verified: true})
	writer := environment.token(t, "ada", ada.Workspace.ID, "read", "write")

	created := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/servers", bearer: writer, body: `{"name":"web-1"}`})
	expectStatus(t, created, http.StatusCreated)
	server := decodeBody[api.CreatedServer](t, created).Server
	if len(server.ID) != 32 {
		t.Fatalf("server ID = %q", server.ID)
	}
	answer := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/apps", bearer: writer, body: `{"name":"blog","server":"` + server.ID + `"}`})
	expectStatus(t, answer, http.StatusCreated)
	app := decodeBody[api.App](t, answer)
	if len(app.ID) != 32 || app.ServerID != server.ID {
		t.Fatalf("app = %+v", app)
	}

	renamed := environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/apps/blog", bearer: writer, body: `{"name":"journal"}`})
	expectStatus(t, renamed, http.StatusOK)
	if got := decodeBody[api.App](t, renamed); got.ID != app.ID || got.Name != "journal" {
		t.Fatalf("renamed = %+v", got)
	}
	fields := expectError(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/apps/journal", bearer: writer, body: `{"name":"Not Valid"}`}), http.StatusUnprocessableEntity, "validation_failed").Fields
	if fields["name"] == "" {
		t.Fatalf("fields = %v", fields)
	}

	for _, ref := range []string{"journal", app.ID} {
		got := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/apps/" + ref, bearer: writer})
		expectStatus(t, got, http.StatusOK)
		if detail := decodeBody[api.AppDetail](t, got); detail.App.ID != app.ID {
			t.Fatalf("%s = %+v", ref, detail.App)
		}
	}
	for _, ref := range []string{"blog", "journal-" + app.ID, "00000000000040008000000000000000"} {
		expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/apps/" + ref, bearer: writer}), http.StatusNotFound, "not_found")
	}

	// A server by its ID, then by its new name.
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/servers/" + server.ID, bearer: writer, body: `{"name":"web-2"}`}), http.StatusOK)
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/servers/web-2/install", bearer: writer}), http.StatusCreated)
}
