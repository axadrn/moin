package api_test

import (
	"net/http"
	"testing"
)

func TestSignOutEverywhereKeepsAPITokens(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "keeper")
	created := createAPIToken(t, environment, owner, `"name":"Script"`)
	other := environment.signIn(t, owner.id)

	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/sessions", cookie: owner.session}), http.StatusNoContent)

	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: other}), http.StatusUnauthorized, "unauthenticated")
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: created.Token}), http.StatusOK)
}
