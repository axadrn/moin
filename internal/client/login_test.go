package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// tokenHub is a hub's token endpoint as RFC 6749 asks: every refresh
// gives a new pair and uses the refresh token up; a used one again ends the
// connection. The account answers only the newest access token.
type tokenHub struct {
	mu        sync.Mutex
	serial    int
	access    map[string]bool
	refresh   map[string]bool
	refreshes int
	ended     bool
}

func newTokenHub() *tokenHub {
	return &tokenHub{access: map[string]bool{"at_0": true}, refresh: map[string]bool{"rt_0": false}}
}

func (h *tokenHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v1/auth/token":
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		token := form.Get("refresh_token")
		used, known := h.refresh[token]
		switch {
		case r.Header.Get("Authorization") != "" || form.Get("grant_type") != "refresh_token":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_request"}`)
			return
		case h.ended || !known:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Log in again."}`)
			return
		case used:
			h.ended = true
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Used twice."}`)
			return
		}
		h.refresh[token] = true
		h.refreshes++
		h.serial++
		access, refresh := fmt.Sprintf("at_%d", h.serial), fmt.Sprintf("rt_%d", h.serial)
		h.access = map[string]bool{access: true}
		h.refresh[refresh] = false
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600,"refresh_token":%q,"connection_id":"c1"}`, access, refresh)
	case "/api/v1/account":
		scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if h.ended || scheme != "Bearer" || !h.access[token] {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthenticated","message":"no"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"user":{"email":"ada@example.com","name":"Ada"}}`)
	}
}

// savedLogin is a login of the hub at address saved where `moin login`
// keeps it, its access token ending in left.
func savedLogin(t *testing.T, address string, left time.Duration) Config {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MOIN_URL", "")
	t.Setenv("MOIN_TOKEN", "")
	expires := time.Now().Add(left)
	config := Config{URL: address, Token: "at_0", RefreshToken: "rt_0", ExpiresAt: &expires, Connection: "c1"}
	if err := SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

// TestClientRefreshesBeforeTheHourEnds and saves the new pair, so the next
// command starts with it.
func TestClientRefreshesBeforeTheHourEnds(t *testing.T) {
	hub := newTokenHub()
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	api, err := New(savedLogin(t, server.URL, 30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Account(context.Background()); err != nil {
		t.Fatalf("Account() error = %v", err)
	}
	saved, err := LoadSaved()
	if err != nil || saved.Token != "at_1" || saved.RefreshToken != "rt_1" || saved.ExpiresAt == nil || time.Until(*saved.ExpiresAt) < 59*time.Minute {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	// An hour is left now: the next request sends the token as it is.
	if _, err := api.Account(context.Background()); err != nil || hub.refreshes != 1 {
		t.Fatalf("second Account() = %v, refreshes %d", err, hub.refreshes)
	}
}

// TestClientRefreshesOnceWhenTheHubSaysNo: an access token the hub no
// longer takes is refreshed once and the request sent again.
func TestClientRefreshesOnceWhenTheHubSaysNo(t *testing.T) {
	hub := newTokenHub()
	hub.access = map[string]bool{}
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	api, err := New(savedLogin(t, server.URL, time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Account(context.Background()); err != nil || hub.refreshes != 1 {
		t.Fatalf("Account() = %v, refreshes %d", err, hub.refreshes)
	}
}

// TestTwoProcessesRefreshOnce: two commands with the same saved login and
// an hour that is over refresh once between them, so the hub never sees
// the refresh token twice and the connection lives on.
func TestTwoProcessesRefreshOnce(t *testing.T) {
	hub := newTokenHub()
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	config := savedLogin(t, server.URL, -time.Minute)
	var wait sync.WaitGroup
	errs := make([]error, 4)
	for index := range errs {
		// Each client is another process: its own copy of the login.
		api, err := New(config)
		if err != nil {
			t.Fatal(err)
		}
		wait.Go(func() {
			_, errs[index] = api.Account(context.Background())
		})
	}
	wait.Wait()
	for index, err := range errs {
		if err != nil {
			t.Errorf("client %d: %v", index, err)
		}
	}
	if hub.ended || hub.refreshes != 1 {
		t.Fatalf("ended %v, refreshes %d, want one refresh and a live connection", hub.ended, hub.refreshes)
	}
}

// TestALoginTheHubEndedSaysToLogInAgain.
func TestALoginTheHubEndedSaysToLogInAgain(t *testing.T) {
	hub := newTokenHub()
	hub.ended = true
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	api, err := New(savedLogin(t, server.URL, -time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Account(context.Background()); !errors.Is(err, ErrLoginEnded) {
		t.Fatalf("Account() = %v, want ErrLoginEnded", err)
	}
}

// TestACITokenNeverRefreshes: a token without a refresh token is sent as
// it is, and a 401 says so.
func TestACITokenNeverRefreshes(t *testing.T) {
	hub := newTokenHub()
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	savedLogin(t, server.URL, time.Hour)
	api, err := New(Config{URL: server.URL, Token: "pat_ci"})
	if err != nil {
		t.Fatal(err)
	}
	var apiErr *Error
	if _, err := api.Account(context.Background()); !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || hub.refreshes != 0 {
		t.Fatalf("Account() = %v, refreshes %d", err, hub.refreshes)
	}
}

// slowHub answers a refresh only after a while: the hub has rotated the
// pair already when the caller gives up.
type slowHub struct{ *tokenHub }

func (h slowHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/auth/token" {
		h.tokenHub.ServeHTTP(w, r)
		return
	}
	recorder := httptest.NewRecorder()
	h.tokenHub.ServeHTTP(recorder, r)
	time.Sleep(300 * time.Millisecond)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(recorder.Code)
	_, _ = w.Write(recorder.Body.Bytes())
}

// TestACancelledCallerDoesNotEndTheLogin: a command stopped while it
// refreshes still saves the new pair, so the next command does not send
// the used refresh token and end the connection.
func TestACancelledCallerDoesNotEndTheLogin(t *testing.T) {
	hub := newTokenHub()
	server := httptest.NewServer(slowHub{hub})
	t.Cleanup(server.Close)
	api, err := New(savedLogin(t, server.URL, 0))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _ = api.Account(ctx)
	saved, err := LoadSaved()
	if err != nil || saved.Token != "at_1" || saved.RefreshToken != "rt_1" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	next, err := New(saved)
	if err != nil {
		t.Fatal(err)
	}
	_, err = next.Account(context.Background())
	hub.mu.Lock()
	ended := hub.ended
	hub.mu.Unlock()
	if err != nil || ended {
		t.Fatalf("the next command: %v, ended %v", err, ended)
	}
}

// TestSaveConfigReplacesTheFile: the config is written beside it and
// renamed, readable only by the user, with nothing left over.
func TestSaveConfigReplacesTheFile(t *testing.T) {
	savedLogin(t, "https://hub.example.com", time.Hour)
	if err := SaveConfig(Config{URL: "https://hub.example.com", Token: "pat_test"}); err != nil {
		t.Fatal(err)
	}
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config = %v, %v", info, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the config folder holds %v, %v", entries, err)
	}
	saved, err := LoadSaved()
	if err != nil || saved.Token != "pat_test" || saved.RefreshToken != "" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
}
