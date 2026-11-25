package api_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/model"
)

// TestConnectionsOverTheAPI: a connection lists the connections of its
// workspace and marks itself, removes one, itself too, and never changes
// rights; the browser does, and the change counts at once.
func TestConnectionsOverTheAPI(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	laptop := environment.login(t, owner, model.DefaultRights)
	desktop := environment.login(t, owner, model.DefaultRights)

	list := decodeBody[api.ConnectionList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/connections", bearer: laptop.AccessToken}))
	if len(list.Items) != 2 {
		t.Fatalf("connections = %+v", list.Items)
	}
	for _, item := range list.Items {
		if item.Current != (item.ID == laptop.ConnectionID) || strings.Join(item.Rights, ",") != "read,deploy,env" {
			t.Fatalf("item = %+v", item)
		}
	}

	// It cannot give itself more.
	grant := apiRequest{method: http.MethodPatch, path: "/api/v1/account/connections/" + laptop.ConnectionID, body: `{"rights":["deploy","env","delete","servers"]}`, bearer: laptop.AccessToken}
	expectError(t, environment.send(t, grant), http.StatusForbidden, "forbidden")
	rename := apiRequest{method: http.MethodPatch, path: "/api/v1/account/name", body: `{"name":"Renamed"}`, bearer: laptop.AccessToken}
	detail := expectError(t, environment.send(t, rename), http.StatusForbidden, "missing_right")
	if detail.Right != "servers" || detail.Message != "This connection may not manage servers and settings. Change it in Settings, Connections." {
		t.Fatalf("detail = %#v", detail)
	}
	grant.bearer, grant.cookie = "", owner.session
	changed := environment.send(t, grant)
	expectStatus(t, changed, http.StatusOK)
	if got := decodeBody[api.Connection](t, changed); strings.Join(got.Rights, ",") != "read,deploy,env,delete,servers" {
		t.Fatalf("rights = %v", got.Rights)
	}
	expectStatus(t, environment.send(t, rename), http.StatusOK)
	expectError(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/connections/" + laptop.ConnectionID, body: `{"rights":["write"]}`, cookie: owner.session}), http.StatusUnprocessableEntity, "validation_failed")

	// Another person's connection is not found.
	other := seedTokenOwner(t, environment, "grace")
	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/connections/" + desktop.ConnectionID, cookie: other.session}), http.StatusNotFound, "not_found")

	// Removing takes access away at once, with read alone.
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/connections/" + desktop.ConnectionID, bearer: laptop.AccessToken}), http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: desktop.AccessToken}), http.StatusUnauthorized, "unauthenticated")
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/connections/" + laptop.ConnectionID, bearer: laptop.AccessToken}), http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: laptop.AccessToken}), http.StatusUnauthorized, "unauthenticated")
}

// TestAConnectionSeesOnlyItsWorkspace: a connection lists the connections
// of its own workspace; the browser lists all of them.
func TestAConnectionSeesOnlyItsWorkspace(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	second, err := environment.workspaces.Create(owner.id, "Second Workspace")
	if err != nil {
		t.Fatal(err)
	}
	first := environment.login(t, owner, model.DefaultRights)
	other := owner
	other.workspaceID = second.ID
	environment.login(t, other, model.DefaultRights)

	list := decodeBody[api.ConnectionList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/connections", bearer: first.AccessToken}))
	if len(list.Items) != 1 || list.Items[0].ID != first.ConnectionID {
		t.Fatalf("connections the connection sees = %+v", list.Items)
	}
	all := decodeBody[api.ConnectionList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/connections", cookie: owner.session}))
	if len(all.Items) != 2 {
		t.Fatalf("connections the browser sees = %+v", all.Items)
	}
	for _, item := range all.Items {
		if item.ID != first.ConnectionID {
			expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account/connections/" + item.ID, bearer: first.AccessToken}), http.StatusNotFound, "not_found")
		}
	}
}

// TestSQLThatWritesNeedsDelete: SQL reads app data, so the query route
// needs env, and SQL that writes the delete right on top, before anything
// reaches the server.
func TestSQLThatWritesNeedsDelete(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	read := apiRequest{method: http.MethodPost, path: "/api/v1/apps/shop/databases/query", body: `{"sql":"SELECT 1"}`, bearer: environment.token(t, owner.id, owner.workspaceID)}
	if detail := expectError(t, environment.send(t, read), http.StatusForbidden, "missing_right"); detail.Right != "env" {
		t.Fatalf("a read without env: %#v", detail)
	}
	read.bearer = environment.token(t, owner.id, owner.workspaceID, "env")
	if recorder := environment.send(t, read); strings.Contains(recorder.Body.String(), "missing_right") {
		t.Fatalf("a read with env was refused for its right: %s", recorder.Body.String())
	}
	write := read
	write.body = `{"sql":"DELETE FROM users","write":true}`
	if detail := expectError(t, environment.send(t, write), http.StatusForbidden, "missing_right"); detail.Right != "delete" {
		t.Fatalf("a write without delete: %#v", detail)
	}
	write.bearer = environment.token(t, owner.id, owner.workspaceID, "env", "delete")
	if recorder := environment.send(t, write); strings.Contains(recorder.Body.String(), "missing_right") {
		t.Fatalf("a write with delete was refused for its right: %s", recorder.Body.String())
	}
}

// TestAStreamEndsWithItsAccessToken: the live stream of a connection ends
// when its access token does, with the event reconnect, so a removal or a
// change of rights counts within the hour. The client comes back with a
// fresh token.
func TestAStreamEndsWithItsAccessToken(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	laptop := environment.login(t, owner, model.DefaultRights)
	// The one connection's access token ends in a second.
	if result := environment.database.MustExec(`UPDATE connection_tokens SET expires_at = $1 WHERE kind = 'access'`, time.Now().UTC().Add(time.Second)); rowsAffected(t, result) != 1 {
		t.Fatal("no access token to shorten")
	}
	started := time.Now()
	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/apps/events", bearer: laptop.AccessToken})
	if took := time.Since(started); took > 10*time.Second {
		t.Fatalf("the stream ran %s", took)
	}
	expectStatus(t, recorder, http.StatusOK)
	if !strings.HasSuffix(recorder.Body.String(), "event: reconnect\ndata: {}\n\n") {
		t.Fatalf("body = %q", recorder.Body.String())
	}

	// A CI token's stream ends within the hour, whenever the token ends.
	expires := time.Now().Add(30 * 24 * time.Hour)
	now := time.Now()
	if got := api.StreamDeadline(model.Bearer{ExpiresAt: &expires}, now); !got.Equal(now.Add(model.AccessTokenLifetime)) {
		t.Fatalf("deadline = %s", got)
	}
	if got := api.StreamDeadline(model.Bearer{}, now); !got.Equal(now.Add(model.AccessTokenLifetime)) {
		t.Fatalf("deadline without an end = %s", got)
	}
}

func rowsAffected(t *testing.T, result sql.Result) int64 {
	t.Helper()
	rows, err := result.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
