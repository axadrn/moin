package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/axadrn/ops/controlplane/internal/api"
	"github.com/axadrn/ops/controlplane/internal/testutil"
)

// TestStatus: a read token sees whether everything is OK, with its issues
// and counts; a server gone offline needs you.
func TestStatus(t *testing.T) {
	environment := newAPIEnvironment(t)
	ada := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Name: "Ada", Verified: true})
	reader := environment.token(t, "ada", ada.Workspace.ID, "read")

	response := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/status", bearer: reader})
	expectStatus(t, response, http.StatusOK)
	status := decodeBody[api.StatusResponse](t, response)
	if !status.OK || len(status.Issues) != 0 || status.Issues == nil || status.Updates == nil {
		t.Fatalf("empty workspace = %+v", status)
	}

	seen := time.Now().Add(-time.Hour)
	if _, err := environment.database.Exec(`INSERT INTO servers (id, workspace_id, name, agent_token_hash, install_token_hash, created_at, last_seen_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`, "web-1-id", ada.Workspace.ID, "web-1", "agent-hash", "install-hash", seen, seen); err != nil {
		t.Fatal(err)
	}
	response = environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/status", bearer: reader})
	expectStatus(t, response, http.StatusOK)
	status = decodeBody[api.StatusResponse](t, response)
	if status.OK || len(status.Issues) != 1 || status.Issues[0].Kind != "server.down" || status.Issues[0].Target != "web-1" || status.Issues[0].Path != "/servers/web-1-id/security" {
		t.Fatalf("server offline = %+v", status)
	}
	if status.Counts.Servers != 1 || status.Counts.ServersOnline != 0 {
		t.Errorf("counts = %+v", status.Counts)
	}
}
