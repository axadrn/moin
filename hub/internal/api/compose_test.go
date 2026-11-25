package api_test

import (
	"net/http"
	"testing"

	"github.com/axadrn/moin/hub/internal/access"
	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// TestGetComposeFile: GET /apps/{app}/compose answers the compose file of
// the live deploy with a read token, not_live before one, and not_found
// for an app of another workspace, by name or ID.
func TestGetComposeFile(t *testing.T) {
	saved := access.Product
	access.Product = access.SelfHosted
	t.Cleanup(func() { access.Product = saved })
	environment := newAPIEnvironment(t)
	ada := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Name: "Ada", Verified: true})
	bob := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "bob", Name: "Bob", Verified: true})
	app := func(token, name string) api.App {
		created := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/servers", bearer: token, body: `{"name":"server-1"}`})
		expectStatus(t, created, http.StatusCreated)
		server := decodeBody[api.CreatedServer](t, created).Server
		answer := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/apps", bearer: token, body: `{"name":"` + name + `","server":"` + server.ID + `"}`})
		expectStatus(t, answer, http.StatusCreated)
		return decodeBody[api.App](t, answer)
	}
	adaWriter := environment.token(t, "ada", ada.Workspace.ID, "read", "write")
	bobWriter := environment.token(t, "bob", bob.Workspace.ID, "read", "write")
	app(adaWriter, "blog")
	shop := app(bobWriter, "shop")
	reader := environment.token(t, "ada", ada.Workspace.ID, "read")

	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/apps/blog/compose", bearer: reader}), http.StatusConflict, "not_live")
	text := "name: blog\nservices:\n  web:\n    image: ghcr.io/me/blog:1\n"
	environment.database.MustExec(`INSERT INTO deploys (id, app_id, number, status, compose_file) SELECT 'd1', id, 1, 'succeeded', $1 FROM apps WHERE name = 'blog'`, text)
	environment.database.MustExec(`INSERT INTO deploys (id, app_id, number, status, compose_file) SELECT 'd2', id, 1, 'succeeded', 'name: shop' FROM apps WHERE name = 'shop'`)
	answer := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/apps/blog/compose", bearer: reader})
	expectStatus(t, answer, http.StatusOK)
	if file := decodeBody[api.ComposeFile](t, answer); file.Number != 1 || file.Text != text {
		t.Fatalf("compose file = %+v", file)
	}
	for _, ref := range []string{"shop", shop.ID} {
		expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/apps/" + ref + "/compose", bearer: reader}), http.StatusNotFound, "not_found")
	}
}
