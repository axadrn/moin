package api_test

import (
	"net/http"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/brand"
	"github.com/axadrn/ops/internal/build"
)

func TestMetaNamesTheProjectTheSupportAddressAndTheVersion(t *testing.T) {
	environment := newAPIEnvironment(t)

	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/meta"})

	expectStatus(t, recorder, http.StatusOK)
	body := decodeBody[map[string]string](t, recorder)
	if len(body) != 3 || body["name"] != brand.Name || body["support_email"] != testSupportEmail || body["version"] != build.Version {
		t.Fatalf("body = %v", body)
	}
}

func TestMetaIsOpenToReadTokens(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "reader")
	created := createAPIToken(t, environment, owner, `"name":"Reader"`)

	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/meta", bearer: created.Token})

	expectStatus(t, recorder, http.StatusOK)
}
