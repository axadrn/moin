package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	case "/api/v1/auth/token":
		form, _ := url.ParseQuery(string(body))
		if r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || form.Get("device_code") != "dc" || form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_request","error_description":"bad poll"}`)
			return
		}
		s.polls = append(s.polls, time.Now())
		answer := s.answers[0]
		s.answers = s.answers[1:]
		if answer == "" {
			_, _ = io.WriteString(w, `{"access_token":"at_new","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_new","scope":"read deploy env","connection_id":"c1"}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": answer, "error_description": answer})
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
	if config.Token != "at_new" || config.RefreshToken != "rt_new" || config.Connection != "c1" || config.ExpiresAt == nil || time.Until(*config.ExpiresAt) < 59*time.Minute || !strings.HasPrefix(config.URL, "http://127.0.0.1") {
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
