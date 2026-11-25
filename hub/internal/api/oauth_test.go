package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

const (
	oauthVerifier = "the-pkce-verifier-of-the-api-test-is-long-enough"
	oauthRedirect = "http://127.0.0.1:33418/callback"
	mcpResource   = "https://app.example.com/mcp"
)

func oauthChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// TestMetadataTellsMCPClientsHowToSignIn: the metadata of /mcp (RFC 9728)
// points at the hub, and the hub's metadata (RFC 8414) names every
// endpoint, PKCE S256 and public clients only.
func TestMetadataTellsMCPClientsHowToSignIn(t *testing.T) {
	environment := newAPIEnvironment(t)
	recorder := environment.send(t, apiRequest{method: http.MethodGet, path: "/.well-known/oauth-protected-resource/mcp"})
	expectStatus(t, recorder, http.StatusOK)
	resource := decodeBody[api.ProtectedResourceMetadata](t, recorder)
	if resource.Resource != mcpResource || len(resource.AuthorizationServers) != 1 || resource.AuthorizationServers[0] != "https://app.example.com" || strings.Join(resource.BearerMethodsSupported, ",") != "header" {
		t.Fatalf("protected resource = %+v", resource)
	}

	recorder = environment.send(t, apiRequest{method: http.MethodGet, path: "/.well-known/oauth-authorization-server"})
	expectStatus(t, recorder, http.StatusOK)
	server := decodeBody[api.AuthorizationServerMetadata](t, recorder)
	want := api.AuthorizationServerMetadata{
		Issuer:                                     "https://app.example.com",
		AuthorizationEndpoint:                      "https://app.example.com/oauth/authorize",
		TokenEndpoint:                              "https://app.example.com/api/v1/auth/token",
		RegistrationEndpoint:                       "https://app.example.com/api/v1/auth/register",
		RevocationEndpoint:                         "https://app.example.com/api/v1/auth/revoke",
		ResponseTypesSupported:                     []string{"code"},
		GrantTypesSupported:                        []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:              []string{"S256"},
		TokenEndpointAuthMethodsSupported:          []string{"none"},
		RevocationEndpointAuthMethodsSupported:     []string{"none"},
		AuthorizationResponseISSParameterSupported: true,
		ServiceDocumentation:                       "https://app.example.com/docs/guides/mcp",
	}
	if !reflect.DeepEqual(server, want) {
		t.Fatalf("authorization server = %+v\nwant %+v", server, want)
	}
	// The resource's metadata is at the root too, for clients that look
	// there.
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/.well-known/oauth-protected-resource"}), http.StatusOK)
}

// register registers an MCP client over the API.
func (e *apiEnvironment) register(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/register", body: body})
}

// TestRegistrationOverTheAPI: an MCP client registers as a public client;
// a redirect URI the hub never sends people to is left out, and without
// one left the answer is invalid_redirect_uri.
func TestRegistrationOverTheAPI(t *testing.T) {
	environment := newAPIEnvironment(t)
	recorder := environment.register(t, `{"client_name":"Cursor","redirect_uris":["cursor://anysphere.cursor-mcp/oauth/callback","http://localhost:8787/callback"],"token_endpoint_auth_method":"client_secret_post","grant_types":["authorization_code","refresh_token"]}`)
	expectStatus(t, recorder, http.StatusCreated)
	client := decodeBody[api.RegisteredClient](t, recorder)
	if client.ClientID == "" || client.ClientName != "Cursor" || strings.Join(client.RedirectURIs, " ") != "http://localhost:8787/callback" || client.TokenEndpointAuthMethod != "none" || client.ClientIDIssuedAt == 0 {
		t.Fatalf("client = %+v", client)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("answer = %v %s", recorder.Header(), recorder.Body.String())
	}
	expectOAuthError(t, environment.register(t, `{"client_name":"x","redirect_uris":["http://example.com/callback"]}`), "invalid_redirect_uri")
	expectOAuthError(t, environment.register(t, `{"client_name":"x"}`), "invalid_redirect_uri")
	expectOAuthError(t, environment.register(t, `not json`), "invalid_client_metadata")
	expectOAuthError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/register", body: "redirect_uris=https://example.com", form: true}), "invalid_client_metadata")
}

// authorize registers a client, has the owner approve it, signed in just
// now, and answers the client and the code.
func (e *apiEnvironment) authorize(t *testing.T, owner tokenOwner, rights model.Rights) (api.RegisteredClient, string) {
	t.Helper()
	recorder := e.register(t, `{"client_name":"Claude Code","redirect_uris":["`+oauthRedirect+`"]}`)
	expectStatus(t, recorder, http.StatusCreated)
	client := decodeBody[api.RegisteredClient](t, recorder)
	authorization, err := e.oauth.Check(service.AuthorizationRequest{ResponseType: "code", ClientID: client.ClientID, RedirectURI: oauthRedirect, State: "s", CodeChallenge: oauthChallenge(oauthVerifier), CodeChallengeMethod: "S256", Resource: mcpResource})
	if err != nil {
		t.Fatal(err)
	}
	session, err := e.auth.AuthenticateSession(owner.session)
	if err != nil {
		t.Fatal(err)
	}
	session.CreatedAt = time.Now()
	target, _, err := e.oauth.Approve(service.OAuthApproval{Authorization: authorization, WorkspaceID: owner.workspaceID, Rights: rights, Presence: service.Presence{Session: session}})
	if err != nil {
		t.Fatal(err)
	}
	back, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	return client, back.Query().Get("code")
}

func codeForm(client api.RegisteredClient, code string) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {oauthVerifier}, "redirect_uri": {oauthRedirect}, "client_id": {client.ClientID}, "resource": {mcpResource}}
}

// TestAuthorizationCodeOverTheAPI: the code gives tokens that work at /mcp
// only; a wrong verifier or resource and a second try are invalid_grant
// and invalid_target, as RFC 6749 and RFC 8707 name them.
func TestAuthorizationCodeOverTheAPI(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	client, code := environment.authorize(t, owner, model.RightDeploy)
	recorder := tokenRequest(t, environment, "/api/v1/auth/token", codeForm(client, code))
	expectStatus(t, recorder, http.StatusOK)
	tokens := decodeBody[api.TokenResponse](t, recorder)
	if !strings.HasPrefix(tokens.AccessToken, "at_") || !strings.HasPrefix(tokens.RefreshToken, "rt_") || tokens.Scope != "read deploy" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("tokens = %+v", tokens)
	}
	// The token's audience is /mcp: the JSON API does not take it.
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: tokens.AccessToken}), http.StatusUnauthorized, "unauthenticated")

	// Refresh: with the client, and the resource if it is sent.
	refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken}, "client_id": {client.ClientID}, "resource": {"https://other.example.com/mcp"}}
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", refresh), "invalid_target")
	refresh.Set("resource", mcpResource)
	recorder = tokenRequest(t, environment, "/api/v1/auth/token", refresh)
	expectStatus(t, recorder, http.StatusOK)
	next := decodeBody[api.TokenResponse](t, recorder)
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", refresh), "invalid_grant")
	if next.ConnectionID != tokens.ConnectionID {
		t.Fatalf("next = %+v", next)
	}
	// The used refresh token ended the connection: its newest refresh
	// token is gone too.
	refresh.Set("refresh_token", next.RefreshToken)
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", refresh), "invalid_grant")
	// So does the code shown a second time.
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", codeForm(client, code)), "invalid_grant")

	for name, change := range map[string]func(url.Values){
		"wrong verifier": func(form url.Values) { form.Set("code_verifier", strings.Repeat("x", 43)) },
		"wrong redirect": func(form url.Values) { form.Set("redirect_uri", "http://127.0.0.1:1/callback") },
	} {
		client, code := environment.authorize(t, owner, model.DefaultRights)
		form := codeForm(client, code)
		change(form)
		expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", form), "invalid_grant")
		expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", codeForm(client, code)), "invalid_grant")
		t.Logf("%s: refused, and the right try after it too", name)
	}
	// A parameter sent twice is refused, as RFC 6749 asks.
	client, code = environment.authorize(t, owner, model.DefaultRights)
	repeated := codeForm(client, code)
	repeated.Add("code_verifier", oauthVerifier)
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", repeated), "invalid_request")

	// The connection names where it goes back.
	list := decodeBody[api.ConnectionList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/connections", cookie: owner.session}))
	if len(list.Items) == 0 || list.Items[0].RedirectHost != "127.0.0.1:33418" {
		t.Fatalf("connections = %+v", list.Items)
	}

	client, code = environment.authorize(t, owner, model.DefaultRights)
	form := codeForm(client, code)
	form.Set("resource", "https://other.example.com/mcp")
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", form), "invalid_target")

	// Revoke ends the connection; its refresh token is gone too.
	client, code = environment.authorize(t, owner, model.DefaultRights)
	recorder = tokenRequest(t, environment, "/api/v1/auth/token", codeForm(client, code))
	expectStatus(t, recorder, http.StatusOK)
	last := decodeBody[api.TokenResponse](t, recorder)
	expectStatus(t, tokenRequest(t, environment, "/api/v1/auth/revoke", url.Values{"token": {last.AccessToken}}), http.StatusOK)
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {last.RefreshToken}, "client_id": {client.ClientID}}), "invalid_grant")
}
