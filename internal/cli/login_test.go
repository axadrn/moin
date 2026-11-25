package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/axadrn/moin/internal/client"
)

// loginServer is a hub that approves a browser login on the
// second poll and knows the token it gave out.
type loginServer struct {
	mu      sync.Mutex
	polls   int
	revoked []string
	removed []string
}

func (s *loginServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method + " " + r.URL.Path {
	case "POST /api/v1/auth/device":
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"hostname"`) || !strings.HasPrefix(r.UserAgent(), "moin-cli/") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"device_code":"dc","user_code":"WDJB-MJHT","verification_uri":"https://moin.example.com/device","verification_uri_complete":"https://moin.example.com/device?code=WDJB-MJHT","expires_in":600,"interval":1}`)
	case "POST /api/v1/auth/token":
		s.polls++
		if s.polls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"authorization_pending","error_description":"Waiting."}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"at_device","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_device","connection_id":"c1"}`)
	case "POST /api/v1/auth/revoke":
		_ = r.ParseForm()
		s.revoked = append(s.revoked, r.PostForm.Get("token"))
	case "GET /api/v1/account/connections":
		_, _ = io.WriteString(w, `{"items":[{"id":"c1","client_name":"moin CLI on laptop-1","rights":["read","deploy","env"],"current":true,"last_used_at":null},{"id":"c2","client_name":"Claude","rights":["read"],"current":false,"last_used_at":null}]}`)
	case "GET /api/v1/ci-tokens":
		_, _ = io.WriteString(w, `{"items":[{"id":"t1","name":"shop deploy","rights":["read","deploy"],"app":"shop","expired":false,"last_used_at":null}]}`)
	case "DELETE /api/v1/account/connections/c2", "DELETE /api/v1/ci-tokens/t1":
		s.removed = append(s.removed, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	case "GET /api/v1/account":
		if r.Header.Get("Authorization") != "Bearer at_device" && r.Header.Get("Authorization") != "Bearer pat_piped" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"user":{"email":"ada@example.com","name":"Ada"}}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func loginEnvironment(t *testing.T) (*loginServer, string) {
	t.Helper()
	server := &loginServer{}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MOIN_URL", "")
	t.Setenv("MOIN_TOKEN", "")
	t.Setenv("NO_COLOR", "1")
	return server, httpServer.URL
}

// TestLoginThroughTheBrowser: moin login shows the code, opens the page,
// polls until the browser approved it and saves the tokens of the
// connection. Logging in again ends the old connection; logout ends the
// new one.
func TestLoginThroughTheBrowser(t *testing.T) {
	server, url := loginEnvironment(t)
	var opened string
	original := openBrowser
	openBrowser = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { openBrowser = original })

	var stdout, stderr strings.Builder
	code := Run([]string{"login", "--url", url, "--web"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "WDJB-MJHT") || !strings.Contains(out, "Logged in as Ada (ada@example.com)") || opened != "https://moin.example.com/device?code=WDJB-MJHT" {
		t.Fatalf("stdout %q, opened %q", out, opened)
	}
	if server.polls != 2 {
		t.Fatalf("polls = %d, want 2", server.polls)
	}
	config, err := client.LoadConfig()
	if err != nil || config.Token != "at_device" || config.RefreshToken != "rt_device" || config.Connection != "c1" || config.URL != url {
		t.Fatalf("saved config = %+v, %v", config, err)
	}

	// The saved login is an older one: logging in again ends it at the hub.
	config.RefreshToken = "rt_older"
	if err := client.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	server.polls = 0
	if code := Run([]string{"login", "--url", url, "--web"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("login again: code %d, stderr %q", code, stderr.String())
	}
	if strings.Join(server.revoked, ",") != "rt_older" {
		t.Fatalf("revoked after the second login = %v, want the older login", server.revoked)
	}

	stdout.Reset()
	if code := Run([]string{"logout"}, strings.NewReader(""), &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Logged out") {
		t.Fatalf("logout: code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	if strings.Join(server.revoked, ",") != "rt_older,rt_device" {
		t.Fatalf("revoked after logout = %v", server.revoked)
	}
	if saved, err := client.LoadSaved(); err != nil || saved.Token != "" {
		t.Fatalf("the login is still saved: %+v, %v", saved, err)
	}
}

// TestLoginWithAPipedToken: a token on stdin logs in without the browser,
// for CI.
func TestLoginWithAPipedToken(t *testing.T) {
	server, url := loginEnvironment(t)
	var stdout, stderr strings.Builder
	code := Run([]string{"login", "--url", url}, strings.NewReader("pat_piped\n"), &stdout, &stderr)
	if code != 0 || server.polls != 0 || !strings.Contains(stdout.String(), "Logged in as Ada") {
		t.Fatalf("code %d, polls %d, stdout %q, stderr %q", code, server.polls, stdout.String(), stderr.String())
	}
	config, err := client.LoadConfig()
	if err != nil || config.Token != "pat_piped" {
		t.Fatalf("saved config = %+v, %v", config, err)
	}
}

// TestConnectionsListsAndRemoves: moin connections shows the connections,
// this login marked, and the CI tokens; rm removes one of either by ID.
func TestConnectionsListsAndRemoves(t *testing.T) {
	server, url := loginEnvironment(t)
	if err := client.SaveConfig(client.Config{URL: url, Token: "pat_piped"}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := Run([]string{"connections"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	for _, part := range []string{"moin CLI on laptop-1 (this login)", "Claude", "shop deploy (only shop)", "read,deploy,env", "ci token"} {
		if !strings.Contains(stdout.String(), part) {
			t.Fatalf("stdout misses %q: %s", part, stdout.String())
		}
	}
	for _, id := range []string{"c2", "t1"} {
		if code := Run([]string{"connections", "rm", id, "--yes"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("rm %s: code %d, stderr %q", id, code, stderr.String())
		}
	}
	if strings.Join(server.removed, ",") != "/api/v1/account/connections/c2,/api/v1/ci-tokens/t1" {
		t.Fatalf("removed = %v", server.removed)
	}
	stderr.Reset()
	if code := Run([]string{"connections", "rm", "nope", "--yes"}, strings.NewReader(""), &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "No connection or CI token has the ID nope") {
		t.Fatalf("rm nope: code %d, stderr %q", code, stderr.String())
	}
}

// TestTheTUILoginEndsTheOldConnection: the TUI saves its login as `moin
// login` does, so the computer's connection from before ends at the hub.
func TestTheTUILoginEndsTheOldConnection(t *testing.T) {
	server, url := loginEnvironment(t)
	if err := client.SaveConfig(client.Config{URL: url, Token: "at_older", RefreshToken: "rt_older", Connection: "c0"}); err != nil {
		t.Fatal(err)
	}
	if err := saveLogin(t.Context(), client.Config{URL: url, Token: "at_device", RefreshToken: "rt_device", Connection: "c1"}); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	revoked := strings.Join(server.revoked, ",")
	server.mu.Unlock()
	if revoked != "rt_older" {
		t.Fatalf("revoked = %q, want the older login", revoked)
	}
	if saved, err := client.LoadSaved(); err != nil || saved.RefreshToken != "rt_device" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
}
