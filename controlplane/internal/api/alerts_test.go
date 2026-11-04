package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/api"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

// fakeWebhook keeps the last body and signature it got; fail answers 500.
type fakeWebhook struct {
	mu        sync.Mutex
	body      string
	signature string
	fail      bool
}

func (f *fakeWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body, f.signature = string(body), r.Header.Get("X-Moin-Signature")
	if f.fail {
		http.Error(w, "nope", http.StatusBadGateway)
	}
}

func (e *apiEnvironment) token(t *testing.T, userID, workspaceID string, scopes ...string) string {
	t.Helper()
	_, raw, err := e.tokens.Create(userID, service.CreateAPITokenInput{Name: "cli", Scopes: scopes, WorkspaceID: workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestAlertDestinations: owners and admins add, test and remove alert
// destinations; members only list them; the URL never comes back.
func TestAlertDestinations(t *testing.T) {
	environment := newAPIEnvironment(t)
	ada := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Name: "Ada", Verified: true})
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "bob", Name: "Bob", Verified: true})
	if _, err := environment.database.Exec(`INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at) VALUES ($1, $2, $3, $4, $5)`, "bob-in-ada", ada.Workspace.ID, "bob", model.WorkspaceRoleMember, time.Now()); err != nil {
		t.Fatal(err)
	}
	owner := environment.token(t, "ada", ada.Workspace.ID, "read", "write")
	reader := environment.token(t, "ada", ada.Workspace.ID, "read")
	member := environment.token(t, "bob", ada.Workspace.ID, "read", "write")
	hook := &fakeWebhook{}
	server := httptest.NewServer(hook)
	defer server.Close()

	created := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/alert-destinations", bearer: owner,
		body: `{"kind":"webhook","name":"ci","url":"` + server.URL + `/in/token123","events":["deploy.failed"]}`})
	expectStatus(t, created, http.StatusCreated)
	destination := decodeBody[api.CreatedAlertDestination](t, created)
	if len(destination.SigningSecret) != 64 || destination.Hint != server.URL+"/…" || strings.Join(destination.Events, ",") != "deploy.failed" || destination.Failing {
		t.Fatalf("created = %+v", destination)
	}
	fields := expectError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/alert-destinations", bearer: owner,
		body: `{"kind":"slack","name":"team","url":"https://example.com/hook"}`}), http.StatusUnprocessableEntity, "validation_failed").Fields
	if fields["url"] == "" {
		t.Fatalf("fields = %v", fields)
	}
	expectError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/alert-destinations", bearer: member,
		body: `{"kind":"webhook","name":"mine","url":"https://example.com/hook"}`}), http.StatusForbidden, "forbidden")
	expectError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/alert-destinations", bearer: reader,
		body: `{"kind":"webhook","name":"mine","url":"https://example.com/hook"}`}), http.StatusForbidden, "insufficient_scope")

	list := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/alert-destinations", bearer: member})
	expectStatus(t, list, http.StatusOK)
	if strings.Contains(list.Body.String(), "token123") || strings.Contains(list.Body.String(), "signing_secret") {
		t.Fatalf("the list shows the target: %s", list.Body)
	}
	if items := decodeBody[api.AlertDestinationList](t, list).Items; len(items) != 1 || items[0].Name != "ci" {
		t.Fatalf("items = %+v", items)
	}

	tested := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/alert-destinations/ci/test", bearer: owner})
	expectStatus(t, tested, http.StatusOK)
	if !strings.HasPrefix(hook.body, `{"event":"test","workspace":"`+ada.Workspace.Slug+`"`) || hook.signature != service.Signature(destination.SigningSecret, []byte(hook.body)) {
		t.Fatalf("webhook got %s, signed %q", hook.body, hook.signature)
	}
	expectError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/alert-destinations/ci/test", bearer: member}), http.StatusForbidden, "forbidden")
	hook.fail = true
	failed := expectError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/alert-destinations/" + destination.ID + "/test", bearer: owner}), http.StatusFailedDependency, "alert_send_failed")
	if failed.Message != "The destination did not take the alert: HTTP 502: nope" {
		t.Fatalf("message = %q", failed.Message)
	}
	if items := decodeBody[api.AlertDestinationList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/alert-destinations", bearer: owner})).Items; !items[0].Failing || items[0].LastError != "HTTP 502: nope" {
		t.Fatalf("after a failed test = %+v", items[0])
	}

	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/alert-destinations/ci", bearer: member}), http.StatusForbidden, "forbidden")
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/alert-destinations/ci", bearer: owner}), http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/alert-destinations/ci", bearer: owner}), http.StatusNotFound, "not_found")
}
