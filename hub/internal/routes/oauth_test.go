package routes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/mcp"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearer adds the access token to every request of the MCP client.
type bearer struct{ token string }

func (b bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(request)
}

// whoami calls the whoami tool at /mcp with token.
func whoami(t *testing.T, server *httptest.Server, token string) (mcp.WhoAmI, error) {
	t.Helper()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{token}}}, nil)
	if err != nil {
		return mcp.WhoAmI{}, err
	}
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "whoami"})
	if err != nil {
		return mcp.WhoAmI{}, err
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var who mcp.WhoAmI
	if err := json.Unmarshal(data, &who); err != nil {
		t.Fatal(err)
	}
	return who, nil
}

func getJSON[T any](t *testing.T, target string) T {
	t.Helper()
	response, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", target, response.StatusCode)
	}
	var value T
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func postForm[T any](t *testing.T, target string, form url.Values) (T, int) {
	t.Helper()
	response, err := http.PostForm(target, form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	var value T
	_ = json.NewDecoder(response.Body).Decode(&value)
	return value, response.StatusCode
}

// TestAnMCPClientSignsInWithOAuth runs what Claude or Cursor do against the
// whole hub, with the person's approval scripted: the 401 of /mcp leads to
// the metadata, the client registers, sends the person to approve with
// PKCE, trades the code, calls a tool with the connection's rights,
// refreshes, and revokes, after which /mcp answers 401 again. A CI token
// keeps working at /mcp as before.
func TestAnMCPClientSignsInWithOAuth(t *testing.T) {
	hub, server := newHub(t)
	ada := testutil.SeedUser(t, hub.DB, testutil.UserOptions{ID: "ada", Name: "Ada", Email: "ada@example.com", Verified: true})
	issuer := "http://localhost:8090"
	// The hub names itself by APP_URL; the test server has its own address.
	local := func(public string) string { return server.URL + strings.TrimPrefix(public, issuer) }

	// 1. /mcp without a token points at its metadata.
	response, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	challenge := response.Header.Get("WWW-Authenticate")
	if response.StatusCode != http.StatusUnauthorized || challenge != `Bearer resource_metadata="`+issuer+`/.well-known/oauth-protected-resource/mcp"` {
		t.Fatalf("401 = %d, %q", response.StatusCode, challenge)
	}
	resource := getJSON[api.ProtectedResourceMetadata](t, local(strings.TrimSuffix(strings.TrimPrefix(challenge, `Bearer resource_metadata="`), `"`)))
	if resource.Resource != issuer+"/mcp" || len(resource.AuthorizationServers) != 1 {
		t.Fatalf("resource metadata = %+v", resource)
	}
	metadata := getJSON[api.AuthorizationServerMetadata](t, local(resource.AuthorizationServers[0]+"/.well-known/oauth-authorization-server"))
	if metadata.Issuer != issuer || strings.Join(metadata.CodeChallengeMethodsSupported, ",") != "S256" {
		t.Fatalf("authorization server metadata = %+v", metadata)
	}

	// 2. The client registers itself.
	redirect := "http://127.0.0.1:43123/callback"
	registration, err := http.Post(local(metadata.RegistrationEndpoint), "application/json", strings.NewReader(`{"client_name":"Claude Code","redirect_uris":["`+redirect+`"],"token_endpoint_auth_method":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	var client api.RegisteredClient
	if err := json.NewDecoder(registration.Body).Decode(&client); err != nil || registration.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d, %v", registration.StatusCode, err)
	}
	_ = registration.Body.Close()

	// 3. It sends the person to approve, with PKCE. A guest signs in
	// first.
	verifier := make([]byte, 32)
	_, _ = rand.Read(verifier)
	codeVerifier := base64.RawURLEncoding.EncodeToString(verifier)
	sum := sha256.Sum256([]byte(codeVerifier))
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {redirect},
		"state":                 {"xyz"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {resource.Resource},
	}
	browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	page, err := browser.Get(local(metadata.AuthorizationEndpoint) + "?" + query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_ = page.Body.Close()
	if page.StatusCode != http.StatusSeeOther || page.Header.Get("Location") != "/auth" {
		t.Fatalf("guest at the authorization endpoint: %d, %q", page.StatusCode, page.Header.Get("Location"))
	}
	// The browser: Ada signed in just now and allows read and deploy.
	session, _, err := hub.AuthService.CreateSession(httptest.NewRequest(http.MethodGet, "/", nil), ada.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := hub.OAuthService.Check(service.ParseAuthorizationRequest(query))
	if err != nil {
		t.Fatal(err)
	}
	target, _, err := hub.OAuthService.Approve(service.OAuthApproval{Authorization: authorization, WorkspaceID: ada.Workspace.ID, Rights: model.RightDeploy, Presence: service.Presence{Session: session}})
	if err != nil {
		t.Fatal(err)
	}
	back, err := url.Parse(target)
	if err != nil || !strings.HasPrefix(target, redirect+"?") || back.Query().Get("state") != "xyz" || back.Query().Get("iss") != issuer {
		t.Fatalf("sent back to %s", target)
	}

	// 4. The client trades the code.
	exchange := url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "code_verifier": {codeVerifier}, "redirect_uri": {redirect}, "client_id": {client.ClientID}, "resource": {resource.Resource}}
	tokens, status := postForm[api.TokenResponse](t, local(metadata.TokenEndpoint), exchange)
	if status != http.StatusOK || !strings.HasPrefix(tokens.AccessToken, "at_") {
		t.Fatalf("token: %d, %+v", status, tokens)
	}

	// 5. It calls a tool with the connection's rights. The JSON API does
	// not take the token: its audience is /mcp.
	who, err := whoami(t, server, tokens.AccessToken)
	if err != nil || who.User.Email != "ada@example.com" || who.Connection.Name != "Claude Code" || strings.Join(who.Connection.Rights, ",") != "read,deploy" {
		t.Fatalf("whoami = %+v, %v", who, err)
	}
	account, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/account", nil)
	if err != nil {
		t.Fatal(err)
	}
	account.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	answer, err := http.DefaultClient.Do(account)
	if err != nil {
		t.Fatal(err)
	}
	_ = answer.Body.Close()
	if answer.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the JSON API took an MCP token: %d", answer.StatusCode)
	}

	// 6. It refreshes; the new token works.
	refreshed, status := postForm[api.TokenResponse](t, local(metadata.TokenEndpoint), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken}, "client_id": {client.ClientID}, "resource": {resource.Resource}})
	if status != http.StatusOK || refreshed.RefreshToken == tokens.RefreshToken || refreshed.ConnectionID != tokens.ConnectionID {
		t.Fatalf("refresh: %d, %+v", status, refreshed)
	}
	if _, err := whoami(t, server, refreshed.AccessToken); err != nil {
		t.Fatalf("whoami after the refresh = %v", err)
	}

	// 7. It revokes: /mcp answers 401 for both of its access tokens.
	if _, status := postForm[struct{}](t, local(metadata.RevocationEndpoint), url.Values{"token": {refreshed.RefreshToken}}); status != http.StatusOK {
		t.Fatalf("revoke: %d", status)
	}
	for _, token := range []string{tokens.AccessToken, refreshed.AccessToken} {
		if _, err := whoami(t, server, token); err == nil || !strings.Contains(err.Error(), "Unauthorized") && !strings.Contains(err.Error(), "401") {
			t.Fatalf("whoami after revoke = %v, want 401", err)
		}
	}

	// A CI token works at /mcp as before.
	_, ciToken, err := hub.CITokenService.Create(service.Actor{UserID: ada.User.ID, WorkspaceID: ada.Workspace.ID}, service.NewCIToken{Name: "agent", Rights: model.DefaultRights})
	if err != nil {
		t.Fatal(err)
	}
	if who, err := whoami(t, server, ciToken); err != nil || who.Connection.Kind != "ci_token" {
		t.Fatalf("whoami with a CI token = %+v, %v", who, err)
	}
}
