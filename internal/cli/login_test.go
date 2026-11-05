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

// loginServer is a control plane that approves a browser login on the
// second poll and knows the token it gave out.
type loginServer struct {
	mu    sync.Mutex
	polls int
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
	case "POST /api/v1/auth/device/token":
		s.polls++
		if s.polls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":"authorization_pending","message":"Waiting."}}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"pat_device","token_type":"Bearer"}`)
	case "GET /api/v1/account":
		if r.Header.Get("Authorization") != "Bearer pat_device" && r.Header.Get("Authorization") != "Bearer pat_piped" {
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
// polls until the browser approved it and saves the device token.
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
	if err != nil || config.Token != "pat_device" || config.URL != url {
		t.Fatalf("saved config = %+v, %v", config, err)
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
