package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

func startDeviceLogin(t *testing.T, environment *apiEnvironment) api.DeviceLogin {
	t.Helper()
	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/device", body: `{"hostname":"host-1"}`})
	expectStatus(t, recorder, http.StatusOK)
	return decodeBody[api.DeviceLogin](t, recorder)
}

// tokenRequest posts a form to the token endpoint, as an OAuth client does.
func tokenRequest(t *testing.T, environment *apiEnvironment, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return environment.send(t, apiRequest{method: http.MethodPost, path: path, body: form.Encode(), form: true})
}

func pollDeviceLogin(t *testing.T, environment *apiEnvironment, deviceCode string) *httptest.ResponseRecorder {
	t.Helper()
	return tokenRequest(t, environment, "/api/v1/auth/token", url.Values{"grant_type": {api.DeviceGrantType}, "device_code": {deviceCode}})
}

// expectOAuthError checks an answer of the token endpoint, as RFC 6749
// says it: 400 and {"error": code}.
func expectOAuthError(t *testing.T, recorder *httptest.ResponseRecorder, code string) {
	t.Helper()
	expectStatus(t, recorder, http.StatusBadRequest)
	if got := decodeBody[api.OAuthError](t, recorder); got.Error != code || got.Description == "" {
		t.Fatalf("error = %#v, want %s", got, code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
}

// approve approves a login as the owner, signed in just now.
func (e *apiEnvironment) approve(t *testing.T, owner tokenOwner, userCode string, rights model.Rights) {
	t.Helper()
	session, err := e.auth.AuthenticateSession(owner.session)
	if err != nil {
		t.Fatal(err)
	}
	session.CreatedAt = time.Now()
	if _, _, err := e.devices.Approve(service.Approval{UserCode: userCode, WorkspaceID: owner.workspaceID, Rights: rights, Presence: service.Presence{Session: session}}); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
}

// login runs the device flow for the owner and answers the tokens.
func (e *apiEnvironment) login(t *testing.T, owner tokenOwner, rights model.Rights) api.TokenResponse {
	t.Helper()
	login := startDeviceLogin(t, e)
	e.approve(t, owner, login.UserCode, rights)
	recorder := pollDeviceLogin(t, e, login.DeviceCode)
	expectStatus(t, recorder, http.StatusOK)
	return decodeBody[api.TokenResponse](t, recorder)
}

func TestDeviceLoginOverTheAPI(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")

	login := startDeviceLogin(t, environment)
	if login.VerificationURI != "https://app.example.com/device" || login.VerificationURIComplete != "https://app.example.com/device?code="+login.UserCode {
		t.Fatalf("verification URIs = %q, %q", login.VerificationURI, login.VerificationURIComplete)
	}
	if login.Interval != 5 || login.ExpiresIn < 590 || login.ExpiresIn > 600 || len(login.DeviceCode) != 64 {
		t.Fatalf("login = %+v", login)
	}
	expectOAuthError(t, pollDeviceLogin(t, environment, login.DeviceCode), "authorization_pending")
	expectOAuthError(t, pollDeviceLogin(t, environment, login.DeviceCode), "slow_down")

	approved := startDeviceLogin(t, environment)
	environment.approve(t, owner, approved.UserCode, model.DefaultRights)
	recorder := pollDeviceLogin(t, environment, approved.DeviceCode)
	expectStatus(t, recorder, http.StatusOK)
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
	tokens := decodeBody[api.TokenResponse](t, recorder)
	if !strings.HasPrefix(tokens.AccessToken, "at_") || !strings.HasPrefix(tokens.RefreshToken, "rt_") || tokens.TokenType != "Bearer" || tokens.ExpiresIn != 3600 || tokens.Scope != "read deploy env" || len(tokens.ConnectionID) != 32 {
		t.Fatalf("tokens = %+v", tokens)
	}
	expectOAuthError(t, pollDeviceLogin(t, environment, approved.DeviceCode), "invalid_grant")

	account := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: tokens.AccessToken})
	expectStatus(t, account, http.StatusOK)
	if !strings.Contains(account.Body.String(), "ada@example.com") {
		t.Fatalf("account = %s", account.Body.String())
	}
	// The connection is listed, named after the computer.
	list := decodeBody[api.ConnectionList](t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/connections", cookie: owner.session}))
	if len(list.Items) != 1 || list.Items[0].ClientName != "moin CLI on host-1" || list.Items[0].ID != tokens.ConnectionID {
		t.Fatalf("connections = %+v", list.Items)
	}

	denied := startDeviceLogin(t, environment)
	if _, err := environment.devices.Deny(denied.UserCode); err != nil {
		t.Fatal(err)
	}
	expectOAuthError(t, pollDeviceLogin(t, environment, denied.DeviceCode), "access_denied")
	expectOAuthError(t, pollDeviceLogin(t, environment, "unknown"), "invalid_grant")
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/token", url.Values{"grant_type": {"password"}, "username": {"ada"}}), "unsupported_grant_type")
	// The token endpoint takes a form, as RFC 6749 says, not JSON.
	expectOAuthError(t, environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/token", body: `{"grant_type":"refresh_token"}`}), "invalid_request")
}

// TestRefreshOverTheAPI: the refresh token gets a new pair once; showing
// it again ends the connection, its newest access token included.
func TestRefreshOverTheAPI(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	first := environment.login(t, owner, model.DefaultRights)

	refresh := func(token string) *httptest.ResponseRecorder {
		return tokenRequest(t, environment, "/api/v1/auth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}})
	}
	recorder := refresh(first.RefreshToken)
	expectStatus(t, recorder, http.StatusOK)
	second := decodeBody[api.TokenResponse](t, recorder)
	if second.AccessToken == first.AccessToken || second.RefreshToken == first.RefreshToken || second.ConnectionID != first.ConnectionID {
		t.Fatalf("second = %+v", second)
	}
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: second.AccessToken}), http.StatusOK)

	expectOAuthError(t, refresh(first.RefreshToken), "invalid_grant")
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: second.AccessToken}), http.StatusUnauthorized, "unauthenticated")
	expectOAuthError(t, refresh(second.RefreshToken), "invalid_grant")
	expectOAuthError(t, refresh("rt_unknown"), "invalid_grant")
	// An access token is no refresh token, and a refresh token calls no API.
	other := environment.login(t, owner, model.DefaultRights)
	expectOAuthError(t, refresh(other.AccessToken), "invalid_grant")
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: other.RefreshToken}), http.StatusUnauthorized, "unauthenticated")
}

// TestRevokeOverTheAPI ends the connection of a token at once, and answers
// 200 for a token it does not know.
func TestRevokeOverTheAPI(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	tokens := environment.login(t, owner, model.DefaultRights)

	expectStatus(t, tokenRequest(t, environment, "/api/v1/auth/revoke", url.Values{"token": {tokens.RefreshToken}}), http.StatusOK)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: tokens.AccessToken}), http.StatusUnauthorized, "unauthenticated")
	expectStatus(t, tokenRequest(t, environment, "/api/v1/auth/revoke", url.Values{"token": {"rt_unknown"}}), http.StatusOK)
	expectOAuthError(t, tokenRequest(t, environment, "/api/v1/auth/revoke", url.Values{}), "invalid_request")
}
