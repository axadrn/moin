package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/testutil"
)

// TestVersionOnTheHostedControlPlane: the hosted control plane answers its
// version and nothing to update, even to an operator, and daemons update by
// themselves until the workspace turns it off.
func TestVersionOnTheHostedControlPlane(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: testAdminEmail, Name: "Ada", Verified: true})
	token := environment.signIn(t, user.User.ID)

	response := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/version", bearer: token})
	if response.Code != http.StatusOK {
		t.Fatalf("GET version = %d %s", response.Code, response.Body)
	}
	var version map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	if version["version"] != "v0.2.0" || version["self_hosted"] != false || version["update_available"] != false || version["can_update"] != false || version["update"] != nil {
		t.Fatalf("version = %v", version)
	}
	update := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/version/update", bearer: token})
	if update.Code != http.StatusConflict || !json.Valid(update.Body.Bytes()) {
		t.Fatalf("POST update = %d %s", update.Code, update.Body)
	}

	auto := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/daemon-updates", bearer: token})
	if auto.Code != http.StatusOK || auto.Body.String() != "{\"auto\":true}\n" {
		t.Fatalf("GET daemon-updates = %d %s", auto.Code, auto.Body)
	}
	off := environment.send(t, apiRequest{method: http.MethodPut, path: "/api/v1/daemon-updates", body: `{"auto":false}`, bearer: token})
	if off.Code != http.StatusOK || off.Body.String() != "{\"auto\":false}\n" {
		t.Fatalf("PUT daemon-updates = %d %s", off.Code, off.Body)
	}
	invalid := environment.send(t, apiRequest{method: http.MethodPut, path: "/api/v1/daemon-updates", body: `{}`, bearer: token})
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT daemon-updates {} = %d %s", invalid.Code, invalid.Body)
	}
}
