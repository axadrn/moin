package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/api"
)

// The API lists passkeys without their credentials and removes them, but
// never the last way in, and only for a session: AI agents do not manage
// ways to sign in.
func TestPasskeysListAndRemove(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "keeper")
	environment.database.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p1', $1, x'01', '{"secret":"credential"}', 'iCloud Keychain')`, owner.id)

	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/passkeys", cookie: owner.session})
	expectStatus(t, recorder, http.StatusOK)
	if strings.Contains(recorder.Body.String(), "credential") {
		t.Fatalf("the list shows the credential: %s", recorder.Body.String())
	}
	list := decodeBody[api.PasskeyList](t, recorder)
	if len(list.Items) != 1 || list.Items[0].ID != "p1" || list.Items[0].Name != "iCloud Keychain" || list.Items[0].LastUsedAt != nil {
		t.Fatalf("items = %#v", list.Items)
	}

	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/passkeys/p1", cookie: owner.session}), http.StatusConflict, "last_way_in")
	environment.database.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p2', $1, x'02', '{}', 'Passkey')`, owner.id)
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/passkeys/p1", cookie: owner.session}), http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/passkeys/p1", cookie: owner.session}), http.StatusNotFound, "not_found")

	created := createAPIToken(t, environment, owner, `"name":"Agent","scopes":["read","write"]`)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/passkeys", bearer: created.Token}), http.StatusForbidden, "forbidden")
	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/passkeys/p2", bearer: created.Token}), http.StatusForbidden, "forbidden")
}
