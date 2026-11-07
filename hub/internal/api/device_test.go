package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/api"
)

func startDeviceLogin(t *testing.T, environment *apiEnvironment) api.DeviceLogin {
	t.Helper()
	recorder := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/device", body: `{"hostname":"host-1"}`})
	expectStatus(t, recorder, http.StatusOK)
	return decodeBody[api.DeviceLogin](t, recorder)
}

func pollDeviceLogin(t *testing.T, environment *apiEnvironment, deviceCode string) *httptest.ResponseRecorder {
	t.Helper()
	return environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/device/token", body: fmt.Sprintf(`{"device_code":%q,"grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`, deviceCode)})
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
	expectError(t, pollDeviceLogin(t, environment, login.DeviceCode), http.StatusBadRequest, "authorization_pending")
	expectError(t, pollDeviceLogin(t, environment, login.DeviceCode), http.StatusBadRequest, "slow_down")

	approved := startDeviceLogin(t, environment)
	if _, err := environment.devices.Approve(owner.id, approved.UserCode, owner.workspaceID); err != nil {
		t.Fatal(err)
	}
	recorder := pollDeviceLogin(t, environment, approved.DeviceCode)
	expectStatus(t, recorder, http.StatusOK)
	token := decodeBody[api.DeviceToken](t, recorder)
	if !rawAPITokenPattern.MatchString(token.AccessToken) || token.TokenType != "Bearer" || token.APIToken.Kind != "device" || token.APIToken.Name != "CLI on host-1" {
		t.Fatalf("token = %+v", token)
	}
	if token.ExpiresIn < 89*24*3600 {
		t.Fatalf("expires_in = %d, want 90 days", token.ExpiresIn)
	}
	expectError(t, pollDeviceLogin(t, environment, approved.DeviceCode), http.StatusBadRequest, "invalid_grant")

	account := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", bearer: token.AccessToken})
	expectStatus(t, account, http.StatusOK)
	if !strings.Contains(account.Body.String(), "ada@example.com") {
		t.Fatalf("account = %s", account.Body.String())
	}
	// The device token is listed with the user's tokens, by its kind.
	list := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account/tokens", bearer: owner.session})
	expectStatus(t, list, http.StatusOK)
	if !strings.Contains(list.Body.String(), `"kind":"device"`) {
		t.Fatalf("tokens = %s", list.Body.String())
	}

	denied := startDeviceLogin(t, environment)
	if _, err := environment.devices.Deny(owner.id, denied.UserCode); err != nil {
		t.Fatal(err)
	}
	expectError(t, pollDeviceLogin(t, environment, denied.DeviceCode), http.StatusBadRequest, "access_denied")
	expectError(t, pollDeviceLogin(t, environment, "unknown"), http.StatusBadRequest, "invalid_grant")

	wrongGrant := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/device/token", body: `{"device_code":"x","grant_type":"password"}`})
	expectError(t, wrongGrant, http.StatusBadRequest, "unsupported_grant_type")
}
