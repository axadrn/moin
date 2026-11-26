package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// After adding a passkey, the security tab asks for its name, filled in
// with the one it got. Save renames it, the form goes and the page
// forgets ?passkey. A wrong name marks the field; another user's passkey
// is not found.
func TestPasskeyNameForm(t *testing.T) {
	test := newDeviceHandlerTest(t, true)
	owner := test.owner.User
	test.db.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p1', $1, x'01', '{}', '1Password')`, owner.ID)
	testutil.SeedUser(t, test.db, testutil.UserOptions{ID: "bob", Email: "bob@example.com", Name: "Bob"})
	test.db.MustExec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name) VALUES ('p2', 'bob', x'02', '{}', 'Passkey')`)
	settings := NewSettingsHandler(test.auth, test.passkeys, nil, nil)
	session := test.session(t, 0)
	base := test.owner.Workspace.Path()

	send := func(method, pattern, target string, form url.Values, run http.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		request := deviceRequest(method, target, form, owner, session)
		request.Header.Set("HX-Request", "true")
		ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{Passkeys: true})
		ctx = ctxkeys.WithURLPath(ctx, request.URL.Path)
		ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
		ctx = ctxkeys.WithWorkspace(ctx, test.owner.Workspace)
		ctx = ctxkeys.WithWorkspaceMember(ctx, &model.WorkspaceMember{WorkspaceID: test.owner.Workspace.ID, UserID: owner.ID, Role: model.WorkspaceRoleOwner})
		mux := http.NewServeMux()
		mux.HandleFunc(method+" "+pattern, run)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request.WithContext(ctx))
		return recorder
	}
	rename := func(id, name string) *httptest.ResponseRecorder {
		return send(http.MethodPatch, "/app/{workspaceSlug}/settings/passkeys/{id}", base+"/settings/passkeys/"+id, url.Values{"name": {name}}, settings.RenamePasskey)
	}

	page := send(http.MethodGet, "/app/{workspaceSlug}/settings/account", base+"/settings/account?tab=security&passkey=p1", nil, settings.SettingsPage)
	for _, want := range []string{"Name this passkey", `hx-patch="` + base + `/settings/passkeys/p1"`, `id="passkey-name"`, `value="1Password"`, `maxlength="64"`, "Rename"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Fatalf("the page after adding lacks %s: %s", want, page.Body.String())
		}
	}
	if page := send(http.MethodGet, "/app/{workspaceSlug}/settings/account", base+"/settings/account?tab=security&passkey=p2", nil, settings.SettingsPage); strings.Contains(page.Body.String(), "Name this passkey") {
		t.Fatal("the page names another user's passkey")
	}

	saved := rename("p1", "  <b>Work laptop</b> ")
	body := saved.Body.String()
	if saved.Code != http.StatusOK || saved.Header().Get("HX-Replace-Url") != base+"/settings/account?tab=security" {
		t.Fatalf("rename = %d, url %q: %s", saved.Code, saved.Header().Get("HX-Replace-Url"), body)
	}
	if !strings.Contains(body, "&lt;b&gt;Work laptop&lt;/b&gt;") || strings.Contains(body, "<b>Work") || strings.Contains(body, "Name this passkey") || !strings.Contains(body, "Name saved") {
		t.Fatalf("the passkeys after the rename: %s", body)
	}

	for _, name := range []string{"   ", strings.Repeat("a", 65)} {
		bad := rename("p1", name)
		if trigger := bad.Header().Get("HX-Trigger"); !strings.Contains(trigger, "passkey-name") || !strings.Contains(trigger, "Name it with 1 to 64 characters.") {
			t.Fatalf("name %q: trigger %q", name, trigger)
		}
	}
	if other := rename("p2", "Mine now"); !strings.Contains(other.Body.String(), "Passkey not found") {
		t.Fatalf("another user's passkey: %s", other.Body.String())
	}
	var name string
	if err := test.db.Get(&name, `SELECT name FROM passkeys WHERE id = 'p2'`); err != nil || name != "Passkey" {
		t.Fatalf("Bob's passkey = %q, %v", name, err)
	}
}
