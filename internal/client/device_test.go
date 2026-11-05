package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// deviceServer answers a login and then the polls in order: each entry is
// an error code, or "" for the token.
type deviceServer struct {
	mu      sync.Mutex
	answers []string
	polls   []time.Time
	started string
}

func (s *deviceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.URL.Path {
	case "/api/v1/auth/device":
		s.started = string(body)
		_, _ = io.WriteString(w, `{"device_code":"dc","user_code":"WDJB-MJHT","verification_uri":"http://x/device","verification_uri_complete":"http://x/device?code=WDJB-MJHT","expires_in":600,"interval":5}`)
	case "/api/v1/auth/device/token":
		if r.Header.Get("Authorization") != "" || !strings.Contains(string(body), `"device_code":"dc"`) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_body","message":"bad poll"}}`)
			return
		}
		s.polls = append(s.polls, time.Now())
		answer := s.answers[0]
		s.answers = s.answers[1:]
		if answer == "" {
			_, _ = io.WriteString(w, `{"access_token":"pat_new","token_type":"Bearer"}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": answer, "message": answer}})
	}
}

func runDeviceLogin(t *testing.T, answers ...string) (*deviceServer, Config, DeviceLogin, error) {
	t.Helper()
	pollUnit = time.Millisecond
	t.Cleanup(func() { pollUnit = time.Second })
	server := &deviceServer{answers: answers}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	var shown DeviceLogin
	config, err := LoginInBrowser(context.Background(), httpServer.URL, "cli", func(login DeviceLogin) { shown = login })
	return server, config, shown, err
}

func TestLoginInBrowserPollsUntilApproved(t *testing.T) {
	server, config, shown, err := runDeviceLogin(t, "authorization_pending", "slow_down", "authorization_pending", "")
	if err != nil {
		t.Fatalf("LoginInBrowser() error = %v", err)
	}
	if config.Token != "pat_new" || !strings.HasPrefix(config.URL, "http://127.0.0.1") {
		t.Fatalf("config = %+v", config)
	}
	if shown.UserCode != "WDJB-MJHT" || shown.VerificationURIComplete != "http://x/device?code=WDJB-MJHT" {
		t.Fatalf("shown = %+v", shown)
	}
	if !strings.Contains(server.started, `"hostname":`) {
		t.Fatalf("start body = %q", server.started)
	}
	if len(server.polls) != 4 {
		t.Fatalf("polls = %d, want 4", len(server.polls))
	}
	// After slow_down the interval grows from 5 to 10 units.
	if gap := server.polls[2].Sub(server.polls[1]); gap < 10*time.Millisecond {
		t.Fatalf("gap after slow_down = %s, want at least 10 units", gap)
	}
}

func TestLoginInBrowserStopsWhenDeniedOrExpired(t *testing.T) {
	for code, want := range map[string]string{
		"access_denied": "denied",
		"expired_token": "expired",
		"invalid_grant": "expired",
	} {
		_, _, _, err := runDeviceLogin(t, "authorization_pending", code)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want %q", code, err, want)
		}
	}
}

func TestLoginInBrowserRefusesPlainHTTP(t *testing.T) {
	_, err := LoginInBrowser(context.Background(), "http://moin.example.com", "cli", func(DeviceLogin) { t.Fatal("started a login over plain http") })
	if err == nil || !strings.Contains(err.Error(), "https://moin.example.com") {
		t.Fatalf("error = %v", err)
	}
}
