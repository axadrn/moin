package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
)

// TestGitHubSetupUpdateFromGitHub: a change of the repos made on github.com
// comes back without a state, and goes back to moin instead of an error.
func TestGitHubSetupUpdateFromGitHub(t *testing.T) {
	h := NewAppsHandler(nil, nil, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/app/github/setup?code=abc&installation_id=177812748&setup_action=update", nil)
	request = request.WithContext(ctxkeys.WithUser(request.Context(), &model.User{ID: "u1"}))
	response := httptest.NewRecorder()
	h.GitHubSetup(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/app" {
		t.Errorf("got %d to %q", response.Code, response.Header().Get("Location"))
	}
}
