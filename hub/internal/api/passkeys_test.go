package api_test

import (
	"net/http"
	"net/http/httptest"
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
	environment.database.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p1', $1, x'01', '{"secret":"credential"}', 'Passkey')`, owner.id)

	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/passkeys", cookie: owner.session})
	expectStatus(t, recorder, http.StatusOK)
	if strings.Contains(recorder.Body.String(), "credential") {
		t.Fatalf("the list shows the credential: %s", recorder.Body.String())
	}
	list := decodeBody[api.PasskeyList](t, recorder)
	if len(list.Items) != 1 || list.Items[0].ID != "p1" || list.Items[0].Name != "Passkey" || list.Items[0].LastUsedAt != nil {
		t.Fatalf("items = %#v", list.Items)
	}

	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/passkeys/p1", cookie: owner.session}), http.StatusConflict, "last_way_in")
	environment.database.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p2', $1, x'02', '{}', 'Passkey')`, owner.id)
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/passkeys/p1", cookie: owner.session}), http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/passkeys/p1", cookie: owner.session}), http.StatusNotFound, "not_found")

	created := createCIToken(t, environment, owner, `"name":"Agent","rights":["deploy","env","delete","servers"]`)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/passkeys", bearer: created.Token}), http.StatusForbidden, "forbidden")
	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/passkeys/p2", bearer: created.Token}), http.StatusForbidden, "forbidden")
}

// PATCH renames your own passkey, trimmed, and refuses an empty name, a
// name too long, another account's passkey and a token.
func TestPasskeysRename(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "keeper")
	other := seedTokenOwner(t, environment, "other")
	environment.database.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p1', $1, x'01', '{}', 'Passkey')`, owner.id)
	environment.database.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p2', $1, x'02', '{}', 'Passkey')`, other.id)
	rename := func(id, body, cookie string) *httptest.ResponseRecorder {
		return environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/passkeys/" + id, body: body, cookie: cookie})
	}

	recorder := rename("p1", `{"name":"  Work laptop "}`, owner.session)
	expectStatus(t, recorder, http.StatusOK)
	if passkey := decodeBody[api.Passkey](t, recorder); passkey.ID != "p1" || passkey.Name != "Work laptop" {
		t.Fatalf("renamed = %#v", passkey)
	}
	for _, body := range []string{`{"name":"  "}`, `{"name":"` + strings.Repeat("a", 65) + `"}`} {
		if detail := expectError(t, rename("p1", body, owner.session), http.StatusUnprocessableEntity, "validation_failed"); detail.Fields["name"] == "" {
			t.Fatalf("%s: fields = %#v", body, detail.Fields)
		}
	}
	expectError(t, rename("p2", `{"name":"Mine now"}`, owner.session), http.StatusNotFound, "not_found")
	created := createCIToken(t, environment, owner, `"name":"Agent","rights":["deploy","env","delete","servers"]`)
	expectError(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/passkeys/p1", body: `{"name":"Agent"}`, bearer: created.Token}), http.StatusForbidden, "forbidden")
}
