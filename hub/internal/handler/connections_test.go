package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/internal/link"
)

type connectionsTest struct {
	deviceHandlerTest
	handler  *ConnectionsHandler
	ciTokens *service.CITokenService
	// signedIn is the owner's session of the requests, signed in just now
	// unless a test says else.
	signedIn *model.Session
}

func newConnectionsTest(t *testing.T) connectionsTest {
	t.Helper()
	device := newDeviceHandlerTest(t, false)
	database := device.db
	box, err := secret.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	servers := service.NewServerService(repository.NewServers(database), repository.NewActivity(database), "https://app.example.com")
	apps := service.NewAppService(repository.NewApps(database, database), servers, box)
	ciTokens := service.NewCITokenService(repository.NewCITokens(database), repository.NewApps(database, database), device.workspaces)
	return connectionsTest{deviceHandlerTest: device, handler: NewConnectionsHandler(device.connections, ciTokens, apps, device.passkeys, device.auth), ciTokens: ciTokens, signedIn: device.session(t, 0)}
}

// request runs the page of pattern at path as the owner, from htmx when
// form is not nil.
func (test connectionsTest) request(t *testing.T, method, pattern, path string, form url.Values, run http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
	}
	ctx := context.Background()
	ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{})
	ctx = ctxkeys.WithURLPath(ctx, path)
	ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
	ctx = ctxkeys.WithUser(ctx, test.owner.User)
	ctx = ctxkeys.WithSession(ctx, test.signedIn)
	ctx = ctxkeys.WithWorkspace(ctx, test.owner.Workspace)
	ctx = ctxkeys.WithWorkspaces(ctx, []model.WorkspaceMembership{{Workspace: *test.owner.Workspace, Role: model.WorkspaceRoleOwner}})
	ctx = ctxkeys.WithWorkspaceMember(ctx, test.owner.WorkspaceMember)
	mux := http.NewServeMux()
	mux.HandleFunc(method+" "+pattern, run)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request.WithContext(ctx))
	return recorder
}

// TestConnectionsPage lists the connections and CI tokens; a right switch
// saves itself and counts at once; Remove ends a connection.
func TestConnectionsPage(t *testing.T) {
	test := newConnectionsTest(t)
	connection, err := test.connections.Create(service.NewConnection{UserID: test.owner.User.ID, WorkspaceID: test.owner.Workspace.ID, ClientName: "moin CLI on <laptop-1>", Rights: model.DefaultRights})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := test.connections.Issue(connection)
	if err != nil {
		t.Fatal(err)
	}
	base := test.owner.Workspace.Path()

	page := test.request(t, http.MethodGet, "/app/{workspaceSlug}/settings/connections", base+"/settings/connections", nil, test.handler.Page)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "moin CLI on &lt;laptop-1&gt;") || !strings.Contains(body, "read, deploy, env") || !strings.Contains(body, "No CI tokens yet.") || strings.Contains(body, "<laptop-1>") {
		t.Fatalf("page: %d %s", page.Code, body)
	}

	path := base + "/settings/connections/" + link.ID(connection.ID)
	detail := test.request(t, http.MethodGet, "/app/{workspaceSlug}/settings/connections/{id}", path, nil, test.handler.Connection)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), path+"/rights/servers") || !strings.Contains(detail.Body.String(), "Remove") {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
	on := test.request(t, http.MethodPost, "/app/{workspaceSlug}/settings/connections/{id}/rights/{right}", path+"/rights/servers", url.Values{"on": {"1"}}, test.handler.SetConnectionRight)
	if on.Code != http.StatusOK || !strings.Contains(on.Body.String(), `value="1" checked`) {
		t.Fatalf("servers on: %d %s", on.Code, on.Body.String())
	}
	off := test.request(t, http.MethodPost, "/app/{workspaceSlug}/settings/connections/{id}/rights/{right}", path+"/rights/deploy", url.Values{}, test.handler.SetConnectionRight)
	if off.Code != http.StatusOK || strings.Contains(off.Body.String(), "checked") {
		t.Fatalf("deploy off: %d %s", off.Code, off.Body.String())
	}
	bearer, err := test.connections.Authenticate(tokens.Access)
	if err != nil || bearer.Rights != model.RightRead|model.RightEnv|model.RightServers {
		t.Fatalf("rights after the switches = %+v, %v", bearer, err)
	}

	removed := test.request(t, http.MethodDelete, "/app/{workspaceSlug}/settings/connections/{id}", path, url.Values{}, test.handler.RemoveConnection)
	if removed.Header().Get("HX-Redirect") != base+"/settings/connections" {
		t.Fatalf("remove: %d %v", removed.Code, removed.Header())
	}
	if _, err := test.connections.Authenticate(tokens.Access); err == nil {
		t.Fatal("the access token works after Remove")
	}
	gone := test.request(t, http.MethodGet, "/app/{workspaceSlug}/settings/connections/{id}", path, nil, test.handler.Connection)
	if gone.Code != http.StatusNotFound {
		t.Fatalf("a removed connection's page = %d", gone.Code)
	}
}

// TestConnectionsPageShowsWhereAnAgentGoesBack: an AI agent's row shows
// the host it sends the person back to next to its name, which is only
// its own word.
func TestConnectionsPageShowsWhereAnAgentGoesBack(t *testing.T) {
	test := newConnectionsTest(t)
	connection, err := test.connections.Create(service.NewConnection{UserID: test.owner.User.ID, WorkspaceID: test.owner.Workspace.ID, ClientName: "Claude", Rights: model.DefaultRights, ClientID: "client-1", Resource: service.MCPPath, RedirectHost: "claude.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	base := test.owner.Workspace.Path()
	page := test.request(t, http.MethodGet, "/app/{workspaceSlug}/settings/connections", base+"/settings/connections", nil, test.handler.Page)
	if !strings.Contains(page.Body.String(), "Claude · claude.example.com") {
		t.Fatalf("page: %d %s", page.Code, page.Body.String())
	}
	path := base + "/settings/connections/" + link.ID(connection.ID)
	detail := test.request(t, http.MethodGet, "/app/{workspaceSlug}/settings/connections/{id}", path, nil, test.handler.Connection)
	if !strings.Contains(detail.Body.String(), "Claude · claude.example.com") {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
}

var revealedToken = regexp.MustCompile(`pat_[a-z2-7]{52}`)

// TestCITokenFromThePage: the form makes a CI token with the rights
// switched on and shows it once; a wrong name stays in the form.
func TestCITokenFromThePage(t *testing.T) {
	test := newConnectionsTest(t)
	base := test.owner.Workspace.Path()
	created := test.request(t, http.MethodPost, "/app/{workspaceSlug}/settings/ci-tokens", base+"/settings/ci-tokens", url.Values{"name": {"shop deploy"}, "expiry": {"30"}, "rights": {"deploy"}}, test.handler.CreateCIToken)
	raw := revealedToken.FindString(created.Body.String())
	if created.Code != http.StatusOK || raw == "" || !strings.Contains(created.Body.String(), "shop deploy") {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	bearer, err := test.ciTokens.Authenticate(raw)
	if err != nil || bearer.Rights != model.RightRead|model.RightDeploy || bearer.ExpiresAt == nil {
		t.Fatalf("the new token = %+v, %v", bearer, err)
	}
	wrong := test.request(t, http.MethodPost, "/app/{workspaceSlug}/settings/ci-tokens", base+"/settings/ci-tokens", url.Values{"name": {" "}, "expiry": {"never"}}, test.handler.CreateCIToken)
	if wrong.Code != http.StatusOK || revealedToken.MatchString(wrong.Body.String()) || !strings.Contains(wrong.Body.String(), "Enter a name") {
		t.Fatalf("a wrong name: %d %s", wrong.Code, wrong.Body.String())
	}

	path := base + "/settings/ci-tokens/" + link.ID(bearer.CITokenID)
	on := test.request(t, http.MethodPost, "/app/{workspaceSlug}/settings/ci-tokens/{id}/rights/{right}", path+"/rights/env", url.Values{"on": {"1"}}, test.handler.SetCITokenRight)
	if on.Code != http.StatusOK || !strings.Contains(on.Body.String(), "checked") {
		t.Fatalf("env on: %d %s", on.Code, on.Body.String())
	}
	if bearer, err := test.ciTokens.Authenticate(raw); err != nil || !bearer.Rights.Has(model.RightEnv) {
		t.Fatalf("rights after the switch = %+v, %v", bearer, err)
	}
	removed := test.request(t, http.MethodDelete, "/app/{workspaceSlug}/settings/ci-tokens/{id}", path, url.Values{}, test.handler.RemoveCIToken)
	if removed.Header().Get("HX-Redirect") != base+"/settings/connections" {
		t.Fatalf("remove: %d %v", removed.Code, removed.Header())
	}
	if _, err := test.ciTokens.Authenticate(raw); err == nil {
		t.Fatal("the CI token works after Remove")
	}
}

// TestMoreRightsNeedAFreshSignIn: with a sign-in older than five minutes
// and no passkey, turning a right on and making a CI token are refused and
// the page asks to sign in again; turning one off and removing are not.
func TestMoreRightsNeedAFreshSignIn(t *testing.T) {
	test := newConnectionsTest(t)
	test.signedIn = test.session(t, 10*time.Minute)
	connection, err := test.connections.Create(service.NewConnection{UserID: test.owner.User.ID, WorkspaceID: test.owner.Workspace.ID, ClientName: "moin CLI on host-1", Rights: model.DefaultRights})
	if err != nil {
		t.Fatal(err)
	}
	base := test.owner.Workspace.Path()
	path := base + "/settings/connections/" + link.ID(connection.ID)

	detail := test.request(t, http.MethodGet, "/app/{workspaceSlug}/settings/connections/{id}", path, nil, test.handler.Connection)
	if !strings.Contains(detail.Body.String(), "Confirm it&#39;s you") || !strings.Contains(detail.Body.String(), base+"/settings/connections/reauth") {
		t.Fatalf("detail asks nothing: %s", detail.Body.String())
	}
	on := test.request(t, http.MethodPost, "/app/{workspaceSlug}/settings/connections/{id}/rights/{right}", path+"/rights/servers", url.Values{"on": {"1"}}, test.handler.SetConnectionRight)
	if strings.Contains(on.Body.String(), `value="1" checked`) || !strings.Contains(on.Body.String(), "Sign in again first") {
		t.Fatalf("servers on: %d %s", on.Code, on.Body.String())
	}
	off := test.request(t, http.MethodPost, "/app/{workspaceSlug}/settings/connections/{id}/rights/{right}", path+"/rights/deploy", url.Values{}, test.handler.SetConnectionRight)
	if off.Code != http.StatusOK || strings.Contains(off.Body.String(), "checked") {
		t.Fatalf("deploy off: %d %s", off.Code, off.Body.String())
	}
	connections, err := test.connections.List(test.owner.User.ID)
	if err != nil || len(connections) != 1 || connections[0].Rights != model.RightRead|model.RightEnv {
		t.Fatalf("connections = %+v, %v", connections, err)
	}
	created := test.request(t, http.MethodPost, "/app/{workspaceSlug}/settings/ci-tokens", base+"/settings/ci-tokens", url.Values{"name": {"shop deploy"}, "expiry": {"30"}, "rights": {"deploy"}}, test.handler.CreateCIToken)
	if revealedToken.MatchString(created.Body.String()) || created.Header().Get("HX-Reswap") != "none" {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}

	test.signedIn = test.session(t, 0)
	fresh := test.request(t, http.MethodGet, "/app/{workspaceSlug}/settings/connections/{id}", path, nil, test.handler.Connection)
	if strings.Contains(fresh.Body.String(), "Confirm it&#39;s you") {
		t.Fatal("a fresh sign-in is asked to confirm")
	}
}
