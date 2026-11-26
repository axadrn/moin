package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A CLI or TUI older than MinClientVersion is told to update, on every API
// path. The current one, builds without a release and every other client
// pass, and so does everything outside the API.
func TestRefuseOutdatedClients(t *testing.T) {
	handler := RefuseOutdatedClients("https://moin.example.com/")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	for _, test := range []struct {
		name, path, userAgent string
		status                int
	}{
		{"old CLI", "/api/v1/apps", "moin-cli/v1.0.0-beta.34", http.StatusUpgradeRequired},
		{"old TUI", "/api/v1/events", "moin-tui/v1.0.0-beta.34", http.StatusUpgradeRequired},
		{"old CLI logs in", "/api/v1/auth/device", "moin-cli/v1.0.0-beta.20", http.StatusUpgradeRequired},
		{"old CLI on an unknown path", "/api/v1/gone", "moin-cli/v1.0.0-beta.34", http.StatusUpgradeRequired},
		{"current CLI", "/api/v1/apps", "moin-cli/" + MinClientVersion, http.StatusTeapot},
		{"newer TUI", "/api/v1/apps", "moin-tui/v1.0.0", http.StatusTeapot},
		{"dev build", "/api/v1/apps", "moin-cli/dev", http.StatusTeapot},
		{"main build", "/api/v1/apps", "moin-tui/main-b4be272", http.StatusTeapot},
		{"curl", "/api/v1/apps", "curl/8.10.1", http.StatusTeapot},
		{"browser", "/api/v1/apps", "Mozilla/5.0", http.StatusTeapot},
		{"no user agent", "/api/v1/apps", "", http.StatusTeapot},
		{"old CLI outside the API", "/cli.sh", "moin-cli/v1.0.0-beta.34", http.StatusTeapot},
		{"daemon", "/daemon/session", "Go-http-client/1.1", http.StatusTeapot},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("User-Agent", test.userAgent)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != test.status {
			t.Errorf("%s: %d, want %d", test.name, recorder.Code, test.status)
			continue
		}
		if test.status != http.StatusUpgradeRequired {
			continue
		}
		var body struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error.Code != "client_outdated" {
			t.Errorf("%s: body %s", test.name, recorder.Body)
		}
		if !strings.Contains(body.Error.Message, "`curl -fsSL https://moin.example.com/cli.sh | sh`") {
			t.Errorf("%s: message %q names no update command", test.name, body.Error.Message)
		}
	}
}

// The OAuth endpoints answer an old CLI in the shape of OAuth, which its
// login reads.
func TestRefuseOutdatedClientsOAuth(t *testing.T) {
	handler := RefuseOutdatedClients("https://moin.example.com")(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/token", nil)
	request.Header.Set("User-Agent", "moin-cli/v1.0.0-beta.34")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var body struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || recorder.Code != http.StatusUpgradeRequired || body.Error != "client_outdated" || !strings.Contains(body.Description, "/cli.sh | sh") {
		t.Errorf("token = %d %s", recorder.Code, recorder.Body)
	}
}
