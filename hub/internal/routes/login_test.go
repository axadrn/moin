package routes

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/app"
	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/axadrn/moin/internal/client"
)

// newHub is the whole hub, as cmd/server runs it, on a fresh database.
func newHub(t *testing.T) (*app.App, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	for key, value := range map[string]string{
		"APP_ENV":       "production",
		"APP_URL":       "http://localhost:8090",
		"DB_CONNECTION": filepath.Join(dir, "hub.db") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		"BILLING":       "false",
		"TELEMETRY":     "false",
		"UPDATE_CHECK":  "false",
		"LOG_HTTP":      "false",
		"SECRETS_KEY":   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)),
		"HOME":          dir,
		// The CLI keeps its login here.
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"MOIN_URL":        "",
		"MOIN_TOKEN":      "",
	} {
		t.Setenv(key, value)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	hub, err := app.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	server := httptest.NewServer(SetupRoutes(hub))
	t.Cleanup(server.Close)
	return hub, server
}

// TestTheCLILogsInAndRefreshesAgainstTheHub runs `moin login` through the
// browser against the whole hub: the person approves with a fresh sign-in
// and picks the rights, the CLI gets a connection, its rights count, it
// refreshes by itself when the hour is over, a refresh token used twice
// ends the connection, and logging out ends it too.
func TestTheCLILogsInAndRefreshesAgainstTheHub(t *testing.T) {
	hub, server := newHub(t)
	ada := testutil.SeedUser(t, hub.DB, testutil.UserOptions{ID: "ada", Name: "Ada", Email: "ada@example.com", Verified: true})
	session, _, err := hub.AuthService.CreateSession(httptest.NewRequest(http.MethodGet, "/", nil), ada.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	login := func() client.Config {
		t.Helper()
		config, err := client.LoginInBrowser(ctx, server.URL, "cli", func(login client.DeviceLogin) {
			// The browser: Ada signed in just now and allows read, deploy
			// and delete.
			_, _, err := hub.DeviceLoginService.Approve(service.Approval{UserCode: login.UserCode, WorkspaceID: ada.Workspace.ID, Rights: model.RightDeploy | model.RightDelete, Presence: service.Presence{Session: session}})
			if err != nil {
				t.Errorf("Approve() error = %v", err)
			}
		})
		if err != nil {
			t.Fatalf("LoginInBrowser() error = %v", err)
		}
		if err := client.SaveConfig(config); err != nil {
			t.Fatal(err)
		}
		return config
	}
	first := login()
	if !strings.HasPrefix(first.Token, "at_") || !strings.HasPrefix(first.RefreshToken, "rt_") || first.Connection == "" {
		t.Fatalf("config = %+v", first)
	}

	api, err := client.New(first)
	if err != nil {
		t.Fatal(err)
	}
	account, err := api.Account(ctx)
	if err != nil || account.Email != "ada@example.com" {
		t.Fatalf("Account() = %+v, %v", account, err)
	}
	if _, err := api.AddServer(ctx, "web-1"); err == nil || !strings.Contains(err.Error(), "This connection may not manage servers and settings. Change it in Settings, Connections.") {
		t.Fatalf("AddServer() without the servers right = %v", err)
	}
	connections, err := api.Connections(ctx)
	if err != nil || len(connections) != 1 || !connections[0].Current || connections[0].ClientName == "" || strings.Join(connections[0].Rights, ",") != "read,deploy,delete" {
		t.Fatalf("Connections() = %+v, %v", connections, err)
	}

	// The hour is over: the next command refreshes by itself and saves the
	// new pair.
	expired := time.Now().Add(-time.Minute)
	first.ExpiresAt = &expired
	if err := client.SaveConfig(first); err != nil {
		t.Fatal(err)
	}
	config, err := client.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	api, err = client.New(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Account(ctx); err != nil {
		t.Fatalf("Account() after the hour = %v", err)
	}
	refreshed, err := client.LoadSaved()
	if err != nil || refreshed.Token == first.Token || refreshed.RefreshToken == first.RefreshToken || refreshed.Connection != first.Connection {
		t.Fatalf("saved after the refresh = %+v, %v", refreshed, err)
	}

	// Someone shows the old refresh token: the hub ends the connection,
	// and the CLI says to log in again.
	reuse, err := http.PostForm(server.URL+"/api/v1/auth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first.RefreshToken}})
	if err != nil {
		t.Fatal(err)
	}
	_ = reuse.Body.Close()
	if reuse.StatusCode != http.StatusBadRequest {
		t.Fatalf("reuse status = %d", reuse.StatusCode)
	}
	api, err = client.New(refreshed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Account(ctx); !errors.Is(err, client.ErrLoginEnded) {
		t.Fatalf("Account() after the reuse = %v, want ErrLoginEnded", err)
	}

	// A new login, then logout: its tokens stop at once.
	second := login()
	if err := client.Logout(ctx, second); err != nil {
		t.Fatal(err)
	}
	api, err = client.New(client.Config{URL: server.URL, Token: second.Token})
	if err != nil {
		t.Fatal(err)
	}
	var apiErr *client.Error
	if _, err := api.Account(ctx); !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("Account() after logout = %v", err)
	}
}
