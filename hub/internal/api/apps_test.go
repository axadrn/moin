package api_test

import (
	"net/http"
	"testing"

	"github.com/axadrn/moin/hub/internal/access"
	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// A canonical domain is for the main service only, here as in MCP; the
// app's password needs a token that may write, and the instant address
// has no password of its own any more.
func TestAppDomainsRules(t *testing.T) {
	// Self-hosted, so the free plan may change apps.
	saved := access.Product
	access.Product = access.SelfHosted
	t.Cleanup(func() { access.Product = saved })
	environment := newAPIEnvironment(t)
	ada := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Name: "Ada", Verified: true})
	writer := environment.token(t, "ada", ada.Workspace.ID, allRights...)
	reader := environment.token(t, "ada", ada.Workspace.ID)

	fields := expectError(t, environment.send(t, apiRequest{method: http.MethodPut, path: "/api/v1/apps/shop/domains", bearer: writer,
		body: `{"service":"api","domains":["api.shop.example.com"],"canonical":"api.shop.example.com"}`}), http.StatusUnprocessableEntity, "validation_failed").Fields
	if fields["canonical"] != "canonical is for the main service only." {
		t.Fatalf("fields = %v", fields)
	}
	expectError(t, environment.send(t, apiRequest{method: http.MethodPut, path: "/api/v1/apps/shop/domains", bearer: writer,
		body: `{"service":"api","domains":["api.shop.example.com"]}`}), http.StatusNotFound, "not_found")

	expectError(t, environment.send(t, apiRequest{method: http.MethodPut, path: "/api/v1/apps/shop/password", bearer: reader, body: `{}`}), http.StatusForbidden, "missing_right")
	expectError(t, environment.send(t, apiRequest{method: http.MethodPut, path: "/api/v1/apps/shop/password", bearer: writer, body: `{}`}), http.StatusNotFound, "not_found")
	expectError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/apps/shop/instant/password", bearer: writer}), http.StatusNotFound, "not_found")
}

// A domain another app or service uses is a validation error that names
// the domain, the same for the web, the API, the CLI and MCP.
func TestDomainUsedNamesTheDomain(t *testing.T) {
	status, detail, known := api.ErrorFor(&repository.DomainUsedError{Name: "example.com"})
	if status != http.StatusUnprocessableEntity || !known || detail.Fields["domains"] != "example.com is already used by another service of this app." {
		t.Fatalf("ErrorFor = %d, %+v, %v", status, detail, known)
	}
}
