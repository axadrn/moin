package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// requestIdentity records what the authentication middleware put into the
// context of one request.
type requestIdentity struct {
	userID  string
	session bool
	token   bool
}

func identify(t *testing.T, environment bearerTestEnvironment, path, authorization string) requestIdentity {
	t.Helper()
	var identity requestIdentity
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		user := ctxkeys.User(r.Context())
		if user != nil {
			identity.userID = user.ID
		}
		identity.session = ctxkeys.Session(r.Context()) != nil
		identity.token = ctxkeys.Bearer(r.Context()) != nil
	})
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	environment.authenticate(environment.tokenAuth(next)).ServeHTTP(httptest.NewRecorder(), request)
	return identity
}

func TestBearersAuthenticateOnlyTheJSONAPIAndMCP(t *testing.T) {
	environment := newBearerTestEnvironment(t)
	fixture := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "claude", Email: "claude@example.com", Name: "Claude", Verified: true})
	_, ciToken, err := environment.ciTokens.Create(service.Actor{UserID: fixture.User.ID, WorkspaceID: fixture.Workspace.ID}, service.NewCIToken{Name: "Claude"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	issue := func(resource string) string {
		t.Helper()
		connection, err := environment.connections.Create(service.NewConnection{UserID: fixture.User.ID, WorkspaceID: fixture.Workspace.ID, ClientName: "CLI", Rights: model.DefaultRights, Resource: resource})
		if err != nil {
			t.Fatal(err)
		}
		tokens, err := environment.connections.Issue(connection)
		if err != nil {
			t.Fatal(err)
		}
		return tokens.Access
	}
	access := issue("")

	for _, raw := range []string{ciToken, access} {
		for _, path := range []string{"/api/v1/account", "/mcp"} {
			if identity := identify(t, environment, path, "Bearer "+raw); identity != (requestIdentity{userID: fixture.User.ID, token: true}) {
				t.Fatalf("%s identity with %.4s = %#v", path, raw, identity)
			}
		}
		for _, path := range []string{"/app", "/app/settings", "/admin", "/auth/logout", "/"} {
			if identity := identify(t, environment, path, "Bearer "+raw); identity != (requestIdentity{}) {
				t.Fatalf("%s identity = %#v, want unauthenticated", path, identity)
			}
		}
	}
	// A connection bound to /mcp, as an AI agent's, works only there.
	agent := issue("/mcp")
	if identity := identify(t, environment, "/mcp", "Bearer "+agent); identity != (requestIdentity{userID: fixture.User.ID, token: true}) {
		t.Fatalf("/mcp identity of an agent = %#v", identity)
	}
	if identity := identify(t, environment, "/api/v1/account", "Bearer "+agent); identity != (requestIdentity{}) {
		t.Fatalf("API identity of an agent = %#v, want unauthenticated", identity)
	}
	for _, raw := range []string{"pat_unknown", "at_unknown", "rt_unknown", "unknown"} {
		if identity := identify(t, environment, "/api/v1/account", "Bearer "+raw); identity != (requestIdentity{}) {
			t.Fatalf("%s identity = %#v", raw, identity)
		}
	}

	session := startCookieSession(t, environment.auth, fixture.User.ID)
	identity := identify(t, environment, "/api/v1/account", "Bearer "+session.Value)
	if identity != (requestIdentity{}) {
		t.Fatalf("session bearer identity = %#v, want unauthenticated", identity)
	}
}
