package api_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/axadrn/moin/internal/link"
)

var rawCITokenPattern = regexp.MustCompile(`^pat_[a-z2-7]{52}$`)

// tokenOwner is a signed-in user who makes CI tokens and connections.
type tokenOwner struct {
	id            string
	session       string
	workspaceID   string
	workspaceSlug string
}

func seedTokenOwner(t *testing.T, environment *apiEnvironment, id string) tokenOwner {
	t.Helper()
	fixture := testutil.SeedUser(t, environment.database, testutil.UserOptions{
		ID:       id,
		Email:    id + "@example.com",
		Name:     strings.ToUpper(id[:1]) + id[1:],
		Verified: true,
	})
	return tokenOwner{
		id:            fixture.User.ID,
		session:       environment.signIn(t, fixture.User.ID),
		workspaceID:   fixture.Workspace.ID,
		workspaceSlug: fixture.Workspace.Slug,
	}
}

// createCIToken makes a CI token with the body's fields, signed in as the
// owner, in the owner's workspace.
func createCIToken(t *testing.T, environment *apiEnvironment, owner tokenOwner, fields string) api.CreatedCIToken {
	t.Helper()
	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/ci-tokens", body: "{" + fields + "}", cookie: owner.session, workspace: owner.workspaceSlug})
	expectStatus(t, recorder, http.StatusCreated)
	return decodeBody[api.CreatedCIToken](t, recorder)
}

func TestCreateCITokenRevealsItOnce(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "maker")

	created := createCIToken(t, environment, owner, `"name":"Nightly export","rights":["deploy","delete"],"expires_in_days":30`)
	if !rawCITokenPattern.MatchString(created.Token) {
		t.Fatalf("token = %q", created.Token)
	}
	token := created.CIToken
	if token.Name != "Nightly export" || token.Prefix != created.Token[4:12] || strings.Join(token.Rights, ",") != "read,deploy,delete" || token.Expired || token.LastUsedAt != nil || token.App != nil || token.CreatedBy != "Maker" {
		t.Fatalf("ci token = %#v", token)
	}
	if token.ExpiresAt == nil || token.ExpiresAt.Sub(token.CreatedAt) != 30*24*time.Hour || token.WorkspaceID != link.ID(owner.workspaceID) {
		t.Fatalf("expires_at = %v, created_at = %v, workspace %s", token.ExpiresAt, token.CreatedAt, token.WorkspaceID)
	}
	defaults := createCIToken(t, environment, owner, `"name":"Export"`)
	if strings.Join(defaults.CIToken.Rights, ",") != "read,deploy,env" || defaults.CIToken.ExpiresAt != nil {
		t.Fatalf("default token = %#v", defaults.CIToken)
	}
	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/ci-tokens", cookie: owner.session})
	list := decodeBody[api.CITokenList](t, recorder)
	if len(list.Items) != 2 || strings.Contains(recorder.Body.String(), created.Token) {
		t.Fatalf("list = %s", recorder.Body.String())
	}
}

func TestCreateCITokenValidates(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "checker")
	for _, test := range []struct {
		body  string
		field string
	}{
		{body: `{"name":"  "}`, field: "name"},
		{body: `{"name":"` + strings.Repeat("n", 65) + `"}`, field: "name"},
		{body: `{"name":"CI","rights":["write"]}`, field: "rights"},
		{body: `{"name":"CI","expires_in_days":0}`, field: "expires_in_days"},
		{body: `{"name":"CI","expires_in_days":366}`, field: "expires_in_days"},
	} {
		recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/ci-tokens", body: test.body, cookie: owner.session})
		detail := expectError(t, recorder, http.StatusUnprocessableEntity, "validation_failed")
		if detail.Fields[test.field] == "" {
			t.Fatalf("%s: fields = %#v", test.body, detail.Fields)
		}
	}
	expectError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/ci-tokens", body: `{"name":"CI","app":"shop"}`, cookie: owner.session}), http.StatusNotFound, "not_found")
	expectError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/ci-tokens", body: `{"name":"CI","owner":"x"}`, cookie: owner.session}), http.StatusBadRequest, "invalid_body")
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/ci-tokens"}), http.StatusUnauthorized, "unauthenticated")
}

func TestCITokenLimit(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "collector")
	for index := range 50 {
		createCIToken(t, environment, owner, fmt.Sprintf(`"name":"Token %d"`, index))
	}
	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/ci-tokens", body: `{"name":"One more"}`, cookie: owner.session})
	expectError(t, recorder, http.StatusConflict, "token_limit")
}

func TestCITokenAuthenticatesAndRecordsUse(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "caller")
	created := createCIToken(t, environment, owner, `"name":"Script"`)

	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: created.Token})
	expectStatus(t, recorder, http.StatusOK)
	if !strings.Contains(recorder.Body.String(), `"id":"`+owner.id+`"`) {
		t.Fatalf("account = %s", recorder.Body.String())
	}
	list := decodeBody[api.CITokenList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/ci-tokens", cookie: owner.session}))
	if list.Items[0].LastUsedAt == nil {
		t.Fatalf("last_used_at was not recorded: %#v", list.Items[0])
	}
	for _, bogus := range []string{"pat_", "pat_" + strings.Repeat("a", 52), created.Token + "x", "at_" + created.Token[4:]} {
		expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: bogus}), http.StatusUnauthorized, "unauthenticated")
	}
}

func TestExpiredCITokenIsUnauthenticated(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "late")
	created := createCIToken(t, environment, owner, `"name":"Short","expires_in_days":1`)
	id, _ := link.Parse(created.CIToken.ID)
	environment.database.MustExec(`UPDATE ci_tokens SET expires_at = $1 WHERE id = $2`, time.Now().Add(-time.Minute).UTC(), id)

	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: created.Token}), http.StatusUnauthorized, "unauthenticated")
	list := decodeBody[api.CITokenList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/ci-tokens", cookie: owner.session}))
	if len(list.Items) != 1 || !list.Items[0].Expired {
		t.Fatalf("items = %#v", list.Items)
	}
}

// TestRemoveCIToken: one's own, from the browser or with the token itself;
// another workspace's token is not found.
func TestRemoveCIToken(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "owner")
	other := seedTokenOwner(t, environment, "other")
	created := createCIToken(t, environment, owner, `"name":"Old"`)
	self := createCIToken(t, environment, owner, `"name":"Self","rights":[]`)
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: created.Token}), http.StatusOK)

	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/ci-tokens/" + created.CIToken.ID, cookie: other.session}), http.StatusNotFound, "not_found")
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/ci-tokens/" + created.CIToken.ID, cookie: owner.session}), http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: created.Token}), http.StatusUnauthorized, "unauthenticated")
	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/ci-tokens/" + created.CIToken.ID, cookie: owner.session}), http.StatusNotFound, "not_found")

	// A read-only token takes itself away: removing needs no more than read.
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/ci-tokens/" + self.CIToken.ID, bearer: self.Token}), http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: self.Token}), http.StatusUnauthorized, "unauthenticated")
}

// TestCITokenRightsChangeOnlyInTheBrowser: a token never gives itself more,
// and a change counts at once.
func TestCITokenRightsChangeOnlyInTheBrowser(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "changer")
	created := createCIToken(t, environment, owner, `"name":"CI","rights":[]`)
	rename := apiRequest{method: http.MethodPatch, path: "/api/v1/account/name", body: `{"name":"Renamed"}`, bearer: created.Token}
	detail := expectError(t, environment.send(t, rename), http.StatusForbidden, "missing_right")
	if detail.Right != "servers" || detail.Message != "This CI token may not manage servers and settings. Change it in Settings, Connections." {
		t.Fatalf("detail = %#v", detail)
	}
	expectError(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/ci-tokens/" + created.CIToken.ID, body: `{"rights":["servers"]}`, bearer: created.Token}), http.StatusForbidden, "forbidden")
	changed := environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/ci-tokens/" + created.CIToken.ID, body: `{"rights":["servers"]}`, cookie: owner.session})
	expectStatus(t, changed, http.StatusOK)
	if got := decodeBody[api.CIToken](t, changed); strings.Join(got.Rights, ",") != "read,servers" {
		t.Fatalf("rights = %v", got.Rights)
	}
	expectStatus(t, environment.send(t, rename), http.StatusOK)
	expectError(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/ci-tokens/" + created.CIToken.ID, body: `{"rights":["root"]}`, cookie: owner.session}), http.StatusUnprocessableEntity, "validation_failed")
}

// TestOwnersSeeAndRemoveEveryCITokenOfTheWorkspace; a member sees their
// own and changes only their own.
func TestOwnersSeeAndRemoveEveryCITokenOfTheWorkspace(t *testing.T) {
	environment := newAPIEnvironment(t)
	seeded := seedTeam(t, environment)
	slug := seeded.workspace.Slug
	owner := tokenOwner{id: "owner", session: seeded.owner, workspaceID: seeded.workspace.ID, workspaceSlug: slug}
	member := tokenOwner{id: "member-user", session: seeded.member, workspaceID: seeded.workspace.ID, workspaceSlug: slug}
	ownersToken := createCIToken(t, environment, owner, `"name":"owner ci"`)
	membersToken := createCIToken(t, environment, member, `"name":"member ci"`)
	list := func(session string) string {
		t.Helper()
		var names []string
		for _, item := range decodeBody[api.CITokenList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/ci-tokens", cookie: session, workspace: slug})).Items {
			names = append(names, item.Name+" by "+item.CreatedBy)
		}
		return strings.Join(names, ", ")
	}
	if got := list(seeded.owner); got != "member ci by member, owner ci by Owner" {
		t.Fatalf("the owner sees %s", got)
	}
	if got := list(seeded.member); got != "member ci by member" {
		t.Fatalf("the member sees %s", got)
	}
	expectError(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/ci-tokens/" + membersToken.CIToken.ID, body: `{"rights":["servers"]}`, cookie: seeded.owner, workspace: slug}), http.StatusForbidden, "forbidden")
	expectError(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/ci-tokens/" + ownersToken.CIToken.ID, cookie: seeded.member, workspace: slug}), http.StatusNotFound, "not_found")
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/ci-tokens/" + membersToken.CIToken.ID, cookie: seeded.owner, workspace: slug}), http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: membersToken.Token}), http.StatusUnauthorized, "unauthenticated")
}

func TestTokensCannotReachSessionOnlyRoutes(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "claude")
	created := createCIToken(t, environment, owner, `"name":"Claude","rights":["deploy","env","delete","servers"]`)
	denied := []apiRequest{
		{method: http.MethodPost, path: "/api/v1/ci-tokens", body: `{"name":"Child"}`},
		{method: http.MethodPatch, path: "/api/v1/ci-tokens/" + created.CIToken.ID, body: `{"rights":["servers"]}`},
		{method: http.MethodPatch, path: "/api/v1/account/connections/unknown", body: `{"rights":["servers"]}`},
		{method: http.MethodGet, path: "/api/v1/account/sessions"},
		{method: http.MethodDelete, path: "/api/v1/account/sessions"},
		{method: http.MethodPatch, path: "/api/v1/account/email", body: `{"email":"new@example.com"}`},
		{method: http.MethodDelete, path: "/api/v1/account", body: `{"confirm_email":"claude@example.com"}`},
		{method: http.MethodPost, path: "/api/v1/auth/logout"},
		{method: http.MethodPost, path: "/api/v1/workspaces", body: `{"name":"Side project"}`},
		{method: http.MethodDelete, path: "/api/v1/workspaces/" + owner.workspaceSlug},
		{method: http.MethodPost, path: "/api/v1/workspaces/" + owner.workspaceSlug + "/leave"},
		{method: http.MethodPost, path: "/api/v1/workspaces/" + owner.workspaceSlug + "/billing/checkout", body: `{"plan_id":"pro"}`},
		{method: http.MethodGet, path: "/api/v1/workspaces/" + owner.workspaceSlug + "/billing/manage"},
		{method: http.MethodGet, path: "/api/v1/admin/users"},
		{method: http.MethodPost, path: "/api/v1/admin/users/" + owner.id + "/sessions/revoke"},
	}
	for _, request := range denied {
		request.bearer = created.Token
		recorder := environment.send(t, request)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) {
			t.Fatalf("%s %s: status %d, body %s", request.method, request.path, recorder.Code, recorder.Body.String())
		}
	}
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: created.Token}), http.StatusOK)
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/ci-tokens", bearer: created.Token}), http.StatusOK)
}

func TestAdminTokenCannotReachAdminRoutes(t *testing.T) {
	environment := newAPIEnvironment(t)
	operator := tokenOwner{id: seedOperator(t, environment)}
	operator.session = environment.signIn(t, operator.id)
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/admin/users", cookie: operator.session}), http.StatusOK)
	created := createCIToken(t, environment, operator, `"name":"Ops","rights":["servers"]`)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/admin/users", bearer: created.Token}), http.StatusForbidden, "forbidden")
}

// TestTokensNeverManageMembers keeps role changes and member removal
// session only. A token of the owner with every right is refused, the
// owner's session still works.
func TestTokensNeverManageMembers(t *testing.T) {
	environment := newAPIEnvironment(t)
	seeded := seedTeam(t, environment)
	owner := tokenOwner{id: "owner", session: seeded.owner, workspaceID: seeded.workspace.ID, workspaceSlug: seeded.workspace.Slug}
	created := createCIToken(t, environment, owner, `"name":"Claude","rights":["deploy","env","delete","servers"]`)
	role := apiRequest{method: http.MethodPatch, path: seeded.path("/members/member-member/role"), body: `{"role":"admin"}`}
	removal := apiRequest{method: http.MethodDelete, path: seeded.path("/members/member-member")}
	for _, request := range []apiRequest{role, removal} {
		request.bearer = created.Token
		expectError(t, environment.send(t, request), http.StatusForbidden, "forbidden")
	}
	role.cookie = seeded.owner
	expectStatus(t, environment.send(t, role), http.StatusOK)
	removal.cookie = seeded.owner
	expectStatus(t, environment.send(t, removal), http.StatusNoContent)
}

func TestCITokenIsBoundToOneWorkspace(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "binder")
	second, err := environment.workspaces.Create(owner.id, "Second Workspace")
	if err != nil {
		t.Fatal(err)
	}
	created := createCIToken(t, environment, owner, `"name":"Bound","rights":["servers"]`)

	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/workspaces/" + owner.workspaceSlug, bearer: created.Token}), http.StatusOK)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/workspaces/" + second.Slug, bearer: created.Token}), http.StatusNotFound, "not_found")
	expectError(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/workspaces/" + second.Slug + "/name", body: `{"name":"Taken over"}`, bearer: created.Token}), http.StatusNotFound, "not_found")
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/workspaces/" + second.Slug + "/activity", bearer: created.Token}), http.StatusNotFound, "not_found")
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/workspaces/" + second.Slug, cookie: owner.session}), http.StatusOK)

	list := decodeBody[struct {
		Items []api.Membership `json:"items"`
	}](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/workspaces", bearer: created.Token}))
	if len(list.Items) != 1 || list.Items[0].Workspace.ID != link.ID(owner.workspaceID) {
		t.Fatalf("token workspaces = %#v", list.Items)
	}
	account := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: created.Token})
	expectStatus(t, account, http.StatusOK)
	if strings.Contains(account.Body.String(), second.ID) {
		t.Fatalf("account shows the other workspace to the token: %s", account.Body.String())
	}
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/workspaces/" + owner.workspaceSlug + "/name", body: `{"name":"Renamed by token"}`, bearer: created.Token}), http.StatusOK)

	environment.database.MustExec(`DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, owner.workspaceID, owner.id)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/workspaces/" + owner.workspaceSlug, bearer: created.Token}), http.StatusNotFound, "not_found")
}
