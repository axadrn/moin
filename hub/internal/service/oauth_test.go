package service

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
)

const (
	testIssuer   = "https://hub.example.com"
	testVerifier = "a-verifier-of-forty-three-characters-or-more_~."
	testRedirect = "https://claude.example.com/callback"
	testLoopback = "http://localhost:4567/callback"
)

// testChallenge is the S256 challenge of testVerifier.
func testChallenge() string {
	sum := sha256.Sum256([]byte(testVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type oauthEnvironment struct {
	deviceLoginEnvironment
	oauth *OAuthService
}

func newOAuthEnvironment(t *testing.T) oauthEnvironment {
	t.Helper()
	e := newDeviceLoginEnvironment(t)
	oauth := NewOAuthService(repository.NewOAuth(e.database), e.connections, e.passkeys, testIssuer+"/")
	oauth.now = e.clock.Now
	return oauthEnvironment{deviceLoginEnvironment: e, oauth: oauth}
}

// register registers a client with the redirect URIs.
func (e oauthEnvironment) register(t *testing.T, redirects ...string) *model.OAuthClient {
	t.Helper()
	client, err := e.oauth.Register(NewClient{ClientName: "Claude", RedirectURIs: redirects})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	return client
}

// request is a good authorization request of the client.
func request(client *model.OAuthClient, redirect string) AuthorizationRequest {
	return AuthorizationRequest{ResponseType: "code", ClientID: client.ID, RedirectURI: redirect, State: "state-1", CodeChallenge: testChallenge(), CodeChallengeMethod: "S256", Resource: testIssuer + "/mcp"}
}

// approve checks the request and approves it as Ada, signed in just now.
// It returns the code from where the person is sent back.
func (e oauthEnvironment) approve(t *testing.T, request AuthorizationRequest, rights model.Rights) (string, *model.Connection) {
	t.Helper()
	authorization, err := e.oauth.Check(request)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	target, connection, err := e.oauth.Approve(OAuthApproval{Authorization: authorization, WorkspaceID: e.user.Workspace.ID, Rights: rights, Presence: Presence{Session: e.fresh(t)}, Source: Source{Via: ViaWeb}})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	back, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	return back.Query().Get("code"), connection
}

// exchange trades code as the client of request.
func (e oauthEnvironment) exchange(request AuthorizationRequest, code string) (Tokens, error) {
	return e.oauth.Exchange(CodeExchange{Code: code, Verifier: testVerifier, RedirectURI: request.RedirectURI, ClientID: request.ClientID, Resource: request.Resource})
}

// TestRegistrationKeepsOnlyRedirectsTheHubSendsPeopleTo: https and http on
// this computer stay; a scheme of the client's own, http elsewhere, a
// fragment and doubles go. Without one left, it fails.
func TestRegistrationKeepsOnlyRedirectsTheHubSendsPeopleTo(t *testing.T) {
	e := newOAuthEnvironment(t)
	client, err := e.oauth.Register(NewClient{ClientName: "  Cursor\n", RedirectURIs: []string{
		"cursor://anysphere.cursor-mcp/oauth/callback",
		testLoopback,
		"http://127.0.0.1/callback",
		"http://[::1]:8080/callback",
		"http://example.com/callback",
		testRedirect,
		testRedirect,
		"https://example.com/callback#part",
		"https://user@example.com/callback",
		"/relative",
	}})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	want := []string{testLoopback, "http://127.0.0.1/callback", "http://[::1]:8080/callback", testRedirect}
	if strings.Join(client.Redirects(), " ") != strings.Join(want, " ") || client.ClientName != "Cursor" {
		t.Fatalf("registered %q as %q, want %q", client.ClientName, client.Redirects(), want)
	}
	stored, err := e.oauth.oauth.Client(client.ID)
	if err != nil || stored.RedirectURIs != client.RedirectURIs {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	for name, redirects := range map[string][]string{
		"none":          nil,
		"http":          {"http://example.com/callback"},
		"custom scheme": {"cursor://anysphere.cursor-mcp/oauth/callback"},
		"too long":      {"https://example.com/" + strings.Repeat("a", maxRedirectURILength)},
	} {
		if _, err := e.oauth.Register(NewClient{RedirectURIs: redirects}); !errors.Is(err, ErrRedirectURIs) {
			t.Errorf("Register(%s) = %v, want ErrRedirectURIs", name, err)
		}
	}
	nameless, err := e.oauth.Register(NewClient{RedirectURIs: []string{testRedirect}})
	if err != nil || nameless.ClientName != "A client" {
		t.Fatalf("a client without a name = %+v, %v", nameless, err)
	}
}

// TestAClientNobodyApprovedGoes: a client never approved goes after a day,
// one still in use stays.
func TestAClientNobodyApprovedGoes(t *testing.T) {
	e := newOAuthEnvironment(t)
	unused := e.register(t, testRedirect)
	used := e.register(t, testRedirect)
	e.approve(t, request(used, testRedirect), model.DefaultRights)
	e.clock.Add(unusedClientLifetime + time.Minute)
	e.register(t, testRedirect)
	if _, err := e.oauth.oauth.Client(unused.ID); !errors.Is(err, repository.ErrOAuthClientNotFound) {
		t.Fatalf("the unused client is still there: %v", err)
	}
	if _, err := e.oauth.oauth.Client(used.ID); err != nil {
		t.Fatalf("the used client went: %v", err)
	}
}

// TestCheckRefusesBadRequests: an unknown client or a redirect URI it did
// not register stays on the hub's page; every other mistake goes back to
// the client with its OAuth error.
func TestCheckRefusesBadRequests(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testRedirect, testLoopback)
	good := request(client, testRedirect)

	for name, change := range map[string]func(*AuthorizationRequest){
		"no client":         func(r *AuthorizationRequest) { r.ClientID = "" },
		"unknown client":    func(r *AuthorizationRequest) { r.ClientID = "nope" },
		"no redirect":       func(r *AuthorizationRequest) { r.RedirectURI = "" },
		"wrong redirect":    func(r *AuthorizationRequest) { r.RedirectURI = "https://evil.example.com/callback" },
		"longer redirect":   func(r *AuthorizationRequest) { r.RedirectURI = testRedirect + "/more" },
		"http elsewhere":    func(r *AuthorizationRequest) { r.RedirectURI = "http://claude.example.com/callback" },
		"other loopback":    func(r *AuthorizationRequest) { r.RedirectURI = "http://127.0.0.1:4567/callback" },
		"loopback path":     func(r *AuthorizationRequest) { r.RedirectURI = "http://localhost:4567/other" },
		"loopback fragment": func(r *AuthorizationRequest) { r.RedirectURI = testLoopback + "#x" },
	} {
		bad := good
		change(&bad)
		authorization, err := e.oauth.Check(bad)
		if authorization != nil || !(errors.Is(err, ErrOAuthClient) || errors.Is(err, ErrOAuthRedirect)) {
			t.Errorf("%s: Check() = %v, %v; want the page's error", name, authorization, err)
		}
	}

	for name, test := range map[string]struct {
		change func(*AuthorizationRequest)
		code   string
	}{
		"token flow":     {func(r *AuthorizationRequest) { r.ResponseType = "token" }, "unsupported_response_type"},
		"no challenge":   {func(r *AuthorizationRequest) { r.CodeChallenge = "" }, "invalid_request"},
		"plain":          {func(r *AuthorizationRequest) { r.CodeChallengeMethod = "plain" }, "invalid_request"},
		"no method":      {func(r *AuthorizationRequest) { r.CodeChallengeMethod = "" }, "invalid_request"},
		"short":          {func(r *AuthorizationRequest) { r.CodeChallenge = "short" }, "invalid_request"},
		"other resource": {func(r *AuthorizationRequest) { r.Resource = "https://other.example.com/mcp" }, "invalid_target"},
		"hub root":       {func(r *AuthorizationRequest) { r.Resource = testIssuer }, "invalid_target"},
	} {
		bad := good
		test.change(&bad)
		authorization, err := e.oauth.Check(bad)
		var refusal *OAuthError
		if authorization == nil || !errors.As(err, &refusal) || refusal.Code != test.code {
			t.Errorf("%s: Check() = %v, %v; want %s back to the client", name, authorization, err, test.code)
			continue
		}
		back, _ := url.Parse(e.oauth.Refuse(authorization, refusal))
		if back.Host != "claude.example.com" || back.Query().Get("error") != test.code || back.Query().Get("state") != "state-1" || back.Query().Get("iss") != testIssuer {
			t.Errorf("%s: sent back to %s", name, back)
		}
	}

	// On this computer only the port may change; the resource may be left
	// out, or written with another case and a slash.
	for name, change := range map[string]func(*AuthorizationRequest){
		"loopback port":  func(r *AuthorizationRequest) { r.RedirectURI = "http://localhost:9999/callback" },
		"no port":        func(r *AuthorizationRequest) { r.RedirectURI = "http://localhost/callback" },
		"no resource":    func(r *AuthorizationRequest) { r.Resource = "" },
		"resource style": func(r *AuthorizationRequest) { r.Resource = "HTTPS://Hub.Example.com/mcp/" },
	} {
		fine := good
		change(&fine)
		if _, err := e.oauth.Check(fine); err != nil {
			t.Errorf("%s: Check() = %v", name, err)
		}
	}
}

// TestCodeGivesTheTokensOfTheConnection: the approved connection is like
// any other, bound to /mcp and to its client, and the code gives its tokens
// once.
func TestCodeGivesTheTokensOfTheConnection(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testLoopback)
	asked := request(client, "http://localhost:9999/callback")
	code, connection := e.approve(t, asked, model.RightDeploy)
	if connection.ClientID != client.ID || connection.Resource != MCPPath || connection.ClientName != "Claude" || connection.Rights != model.RightRead|model.RightDeploy {
		t.Fatalf("connection = %+v", connection)
	}
	tokens, err := e.exchange(asked, code)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	bearer, err := e.connections.Authenticate(tokens.Access)
	if err != nil || bearer.ConnectionID != connection.ID || bearer.Resource != MCPPath {
		t.Fatalf("Authenticate() = %+v, %v", bearer, err)
	}
	if !e.recorded(t, "connection.approved", "read,deploy") {
		t.Fatal("the approval is not in the activity")
	}
	// The refresh token rotates and needs the same client.
	if _, err := e.connections.Refresh(tokens.Refresh, "another"); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("Refresh() by another client = %v", err)
	}
	next, err := e.connections.Refresh(tokens.Refresh, client.ID)
	if err != nil || next.Refresh == tokens.Refresh {
		t.Fatalf("Refresh() = %+v, %v", next, err)
	}
	if _, err := e.connections.Refresh(tokens.Refresh, client.ID); !errors.Is(err, ErrRefreshReused) {
		t.Fatalf("Refresh() twice = %v, want ErrRefreshReused", err)
	}
	if _, err := e.connections.Authenticate(next.Access); err == nil {
		t.Fatal("the newest access token works after the reuse")
	}
}

// TestCodeWorksForOneGoodTry: a wrong verifier, client, redirect URI or
// resource, a late code and a second try all fail, and each ends the
// connection: a client that does it right never makes such a mistake.
func TestCodeWorksForOneGoodTry(t *testing.T) {
	for name, test := range map[string]struct {
		change func(*CodeExchange)
		wait   time.Duration
		want   error
	}{
		"wrong verifier": {change: func(x *CodeExchange) { x.Verifier = strings.Repeat("v", 43) }, want: ErrCodeInvalid},
		"no verifier":    {change: func(x *CodeExchange) { x.Verifier = "" }, want: ErrCodeInvalid},
		"other client":   {change: func(x *CodeExchange) { x.ClientID = "another" }, want: ErrCodeInvalid},
		"no redirect":    {change: func(x *CodeExchange) { x.RedirectURI = "" }, want: ErrCodeInvalid},
		"other redirect": {change: func(x *CodeExchange) { x.RedirectURI = "http://localhost:1111/callback" }, want: ErrCodeInvalid},
		"other resource": {change: func(x *CodeExchange) { x.Resource = "https://other.example.com/mcp" }, want: ErrResourceInvalid},
		"late":           {wait: model.OAuthCodeLifetime + time.Second, want: ErrCodeInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			e := newOAuthEnvironment(t)
			client := e.register(t, testLoopback)
			asked := request(client, testLoopback)
			code, connection := e.approve(t, asked, model.DefaultRights)
			e.clock.Add(test.wait)
			exchange := CodeExchange{Code: code, Verifier: testVerifier, RedirectURI: asked.RedirectURI, ClientID: client.ID, Resource: asked.Resource}
			if test.change != nil {
				test.change(&exchange)
			}
			if _, err := e.oauth.Exchange(exchange); !errors.Is(err, test.want) {
				t.Fatalf("Exchange() = %v, want %v", err, test.want)
			}
			// The right try after the wrong one is too late.
			if _, err := e.exchange(asked, code); !errors.Is(err, ErrCodeInvalid) {
				t.Fatalf("Exchange() after a wrong try = %v", err)
			}
			if _, err := e.connections.connections.ByID(connection.ID); !errors.Is(err, repository.ErrConnectionNotFound) {
				t.Fatalf("the connection is still live: %v", err)
			}
		})
	}
}

// TestACodeShownTwiceEndsTheConnection: the second trade of a code ends
// the connection, with the tokens the first trade got (OAuth 2.1, 4.1.3).
func TestACodeShownTwiceEndsTheConnection(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testRedirect)
	asked := request(client, testRedirect)
	code, _ := e.approve(t, asked, model.DefaultRights)
	tokens, err := e.exchange(asked, code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.exchange(asked, code); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("second Exchange() = %v, want ErrCodeInvalid", err)
	}
	if _, err := e.connections.Authenticate(tokens.Access); err == nil {
		t.Fatal("the first trade's access token works after the reuse")
	}
	if !e.recorded(t, "connection.ended", "the authorization code was used twice") {
		t.Fatal("the activity does not say why the connection ended")
	}
	for _, code := range []string{"", "unknown"} {
		if _, err := e.exchange(asked, code); !errors.Is(err, ErrCodeInvalid) {
			t.Errorf("Exchange(%q) = %v", code, err)
		}
	}
}

// TestACodeNobodyTradedEndsItsConnection: a client that gave up leaves no
// connection without tokens behind.
func TestACodeNobodyTradedEndsItsConnection(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testRedirect)
	_, connection := e.approve(t, request(client, testRedirect), model.DefaultRights)
	e.clock.Add(model.OAuthCodeLifetime + time.Second)
	e.approve(t, request(client, testRedirect), model.DefaultRights)
	if _, err := e.connections.connections.ByID(connection.ID); !errors.Is(err, repository.ErrConnectionNotFound) {
		t.Fatalf("the untraded connection is still live: %v", err)
	}
	if !e.recorded(t, "connection.ended", "the authorization code was never used") {
		t.Fatal("the activity does not say why the connection ended")
	}
}

// TestOAuthApprovalNeedsPresenceAndAWorkspaceOfOnesOwn: the approval is
// the one of `moin login`: a fresh confirmation or the passkey right then,
// never an impersonation, and only into one's own workspace.
func TestOAuthApprovalNeedsPresenceAndAWorkspaceOfOnesOwn(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testRedirect)
	authorization, err := e.oauth.Check(request(client, testRedirect))
	if err != nil {
		t.Fatal(err)
	}
	approve := func(workspaceID string, presence Presence) error {
		_, _, err := e.oauth.Approve(OAuthApproval{Authorization: authorization, WorkspaceID: workspaceID, Rights: model.DefaultRights, Presence: presence})
		return err
	}
	old := e.fresh(t)
	old.ConfirmedAt = e.clock.now.Add(-ConfirmedFor - time.Second)
	if err := approve(e.user.Workspace.ID, Presence{Session: old}); !errors.Is(err, ErrConfirm) {
		t.Fatalf("Approve() with an old confirmation = %v", err)
	}
	operator := "operator"
	impersonated := e.fresh(t)
	impersonated.ImpersonatorUserID = &operator
	if err := approve(e.user.Workspace.ID, Presence{Session: impersonated}); !errors.Is(err, ErrImpersonating) {
		t.Fatalf("Approve() while impersonating = %v", err)
	}
	grace := testutil.SeedUser(t, e.database, testutil.UserOptions{ID: "grace", Name: "Grace", Verified: true})
	if err := approve(grace.Workspace.ID, Presence{Session: e.fresh(t)}); !errors.Is(err, ErrConnectionWorkspace) {
		t.Fatalf("Approve() into another person's workspace = %v", err)
	}
	connections, err := e.connections.List(e.user.User.ID)
	if err != nil || len(connections) != 0 {
		t.Fatalf("a refused approval made a connection: %+v, %v", connections, err)
	}

	// With a passkey, only its answer right then counts.
	session := e.fresh(t)
	authenticator := newSoftAuthenticator(t)
	e.add(t, session, authenticator)
	if err := approve(e.user.Workspace.ID, Presence{Session: session}); !errors.Is(err, ErrPresencePasskey) {
		t.Fatalf("Approve() without the passkey = %v", err)
	}
	options, ceremony, err := e.passkeys.BeginConfirm(session)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := io.ReadAll(authenticator.get(t, options, passkeyOrigin))
	if err != nil {
		t.Fatal(err)
	}
	if err := approve(e.user.Workspace.ID, Presence{Session: session, Ceremony: ceremony, Answer: answer}); err != nil {
		t.Fatalf("Approve() with the passkey = %v", err)
	}
}

// TestDenyGoesBackWithAccessDenied: Cancel sends the person back, and the
// client hears access_denied with its state.
func TestDenyGoesBackWithAccessDenied(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, "https://claude.example.com/callback?keep=1")
	authorization, err := e.oauth.Check(request(client, "https://claude.example.com/callback?keep=1"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := url.Parse(e.oauth.Deny(authorization))
	if err != nil {
		t.Fatal(err)
	}
	query := back.Query()
	if back.Host != "claude.example.com" || back.Path != "/callback" || query.Get("keep") != "1" || query.Get("error") != "access_denied" || query.Get("state") != "state-1" || query.Get("iss") != testIssuer || query.Get("code") != "" {
		t.Fatalf("Deny() = %s", back)
	}
}

// TestRedirectHostsArePlainASCII: a host in other letters can pass for
// another one, so it is refused, also percent-encoded. Punycode stays and
// shows as it is.
func TestRedirectHostsArePlainASCII(t *testing.T) {
	e := newOAuthEnvironment(t)
	for _, uri := range []string{"https://cl\u0430ude.example.com/callback", "https://%D1%81laude.example.com/callback", "http://l\u043ecalhost/callback"} {
		if _, err := e.oauth.Register(NewClient{RedirectURIs: []string{uri}}); !errors.Is(err, ErrRedirectURIs) {
			t.Errorf("Register(%q) = %v, want ErrRedirectURIs", uri, err)
		}
	}
	client := e.register(t, "https://xn--clude-8va.example.com/callback")
	authorization, err := e.oauth.Check(request(client, "https://xn--clude-8va.example.com/callback"))
	if err != nil || authorization.RedirectHost() != "xn--clude-8va.example.com" {
		t.Fatalf("punycode: %v, %v", authorization, err)
	}
}

// TestRedirectHostShowsAPortThatIsNotTheDefault: the page and the
// connection name the port only when it is not the scheme's own.
func TestRedirectHostShowsAPortThatIsNotTheDefault(t *testing.T) {
	for uri, want := range map[string]string{
		"https://claude.example.com/callback":      "claude.example.com",
		"https://claude.example.com:443/callback":  "claude.example.com",
		"https://claude.example.com:8443/callback": "claude.example.com:8443",
		"http://localhost:5555/callback":           "localhost:5555",
		"http://localhost:80/callback":             "localhost",
		"http://[::1]:5555/callback":               "[::1]:5555",
	} {
		authorization := Authorization{Request: AuthorizationRequest{RedirectURI: uri}}
		if got := authorization.RedirectHost(); got != want {
			t.Errorf("RedirectHost(%s) = %q, want %q", uri, got, want)
		}
	}
	if !(Authorization{Request: AuthorizationRequest{RedirectURI: "http://[::1]:5555/callback"}}).Local() {
		t.Error("[::1] is not local")
	}
}

// TestClientNamesCannotHideOrTurnText: control and format characters, like
// a right-to-left override or a zero-width space, and quotes go.
func TestClientNamesCannotHideOrTurnText(t *testing.T) {
	for name, want := range map[string]string{
		"Claude\u202e edoc":          "Claude edoc",
		"Cla\u200bude":               "Claude",
		"\u201cClaude\u201d \"Pro\"": "Claude Pro",
		"\ufeff":                     "A client",
		"Claude\u2066x\u2069":        "Claudex",
	} {
		if got := CleanClientName(name); got != want {
			t.Errorf("CleanClientName(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestTheConnectionKeepsWhereItGoesBack: the connection names its redirect
// host, and so does the activity.
func TestTheConnectionKeepsWhereItGoesBack(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testRedirect)
	_, connection := e.approve(t, request(client, testRedirect), model.DefaultRights)
	if connection.RedirectHost != "claude.example.com" || connection.Label() != "Claude · claude.example.com" {
		t.Fatalf("connection = %+v", connection)
	}
	listed, err := e.connections.List(e.user.User.ID)
	if err != nil || len(listed) != 1 || listed[0].RedirectHost != "claude.example.com" {
		t.Fatalf("List() = %+v, %v", listed, err)
	}
	if !e.recorded(t, "connection.approved", "Claude · claude.example.com") {
		t.Fatal("the activity does not name the host")
	}
}

// TestAConnectionWhoseTokensFailEnds: the code is used up once traded, so
// when its tokens cannot be made, the connection goes.
func TestAConnectionWhoseTokensFailEnds(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testRedirect)
	asked := request(client, testRedirect)
	code, connection := e.approve(t, asked, model.DefaultRights)
	e.database.MustExec(`CREATE TRIGGER no_tokens BEFORE INSERT ON connection_tokens BEGIN SELECT RAISE(ABORT, 'no tokens'); END`)
	if _, err := e.exchange(asked, code); err == nil {
		t.Fatal("Exchange() worked without tokens")
	}
	if _, err := e.connections.connections.ByID(connection.ID); !errors.Is(err, repository.ErrConnectionNotFound) {
		t.Fatalf("the connection is still live: %v", err)
	}
}

// TestListingEndsUntradedConnections: Settings never shows an AI agent's
// connection whose code nobody traded.
func TestListingEndsUntradedConnections(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testRedirect)
	e.approve(t, request(client, testRedirect), model.DefaultRights)
	e.clock.Add(model.OAuthCodeLifetime + time.Second)
	listed, err := e.connections.List(e.user.User.ID)
	if err != nil || len(listed) != 0 {
		t.Fatalf("List() = %+v, %v", listed, err)
	}
	if !e.recorded(t, "connection.ended", "the authorization code was never used") {
		t.Fatal("the activity does not say why the connection ended")
	}
}

// TestARepeatedParameterIsRefused: OAuth sends each parameter once. A
// repeated client or redirect URI stays on the hub's page; any other goes
// back as invalid_request.
func TestARepeatedParameterIsRefused(t *testing.T) {
	e := newOAuthEnvironment(t)
	client := e.register(t, testRedirect)
	values := request(client, testRedirect).Values()
	for name, want := range map[string]error{"client_id": ErrOAuthClient, "redirect_uri": ErrOAuthRedirect} {
		repeated := url.Values{}
		for key, value := range values {
			repeated[key] = append([]string{}, value...)
		}
		repeated.Add(name, repeated.Get(name))
		if _, err := e.oauth.Check(ParseAuthorizationRequest(repeated)); !errors.Is(err, want) {
			t.Errorf("repeated %s: Check() = %v, want %v", name, err, want)
		}
	}
	for _, name := range []string{"state", "code_challenge", "resource", "response_type"} {
		repeated := url.Values{}
		for key, value := range values {
			repeated[key] = append([]string{}, value...)
		}
		repeated.Add(name, "x")
		authorization, err := e.oauth.Check(ParseAuthorizationRequest(repeated))
		var refusal *OAuthError
		if authorization == nil || !errors.As(err, &refusal) || refusal.Code != "invalid_request" {
			t.Errorf("repeated %s: Check() = %v, %v", name, authorization, err)
		}
	}
}
