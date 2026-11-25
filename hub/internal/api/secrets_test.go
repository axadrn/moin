package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// TestSecretsLiveOnServers: values go to a server and never come back.
// A new app answers its password once, with protected on; turning the
// password off and on again without one answers a new one once. No route
// for an instant password is left. Env and registry logins go to the
// server, which must be online: an offline one answers 409 server_offline
// in its own words.
// Registries and integrations say which servers keep a secret.
func TestSecretsLiveOnServers(t *testing.T) {
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "owner"})
	box, err := secret.New(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	servers := service.NewServerService(repository.NewServers(database), repository.NewActivity(database), "https://moin.example.com")
	apps := service.NewAppService(repository.NewApps(database, database), servers, box)
	actor := service.Actor{WorkspaceID: "workspace-owner", UserID: "owner"}
	online, _, err := servers.Create(actor, "server-1")
	if err != nil {
		t.Fatal(err)
	}
	servers.Connect(online, testutil.Hello, "203.0.113.5", func() {}, testutil.AnswerOK(servers.Receive, online.ID))

	handler := NewAppsHandler(apps)
	mux := http.NewServeMux()
	for pattern, serve := range map[string]http.HandlerFunc{
		"POST /api/v1/apps":                    handler.Create,
		"GET /api/v1/apps/{app}/instant":       handler.GetInstant,
		"PUT /api/v1/apps/{app}/protection":    handler.SetProtection,
		"DELETE /api/v1/apps/{app}/protection": handler.DeleteProtection,
		"PATCH /api/v1/apps/{app}/env":         handler.UpdateEnv,
		"GET /api/v1/registries":               handler.Registries,
		"PUT /api/v1/registries/{host}":        handler.SetRegistry,
		"GET /api/v1/integrations":             handler.GetIntegrations,
		"PUT /api/v1/integrations":             handler.SetIntegrations,
	} {
		mux.HandleFunc(pattern, serve)
	}
	send := func(method, path, body string) (int, map[string]any, string) {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		ctx := ctxkeys.WithWorkspace(ctxkeys.WithUser(request.Context(), &model.User{ID: "owner"}), &model.Workspace{ID: "workspace-owner"})
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request.WithContext(ctx))
		answer := map[string]any{}
		_ = json.Unmarshal(recorder.Body.Bytes(), &answer)
		return recorder.Code, answer, recorder.Body.String()
	}
	errorOf := func(answer map[string]any) (string, string) {
		detail, _ := answer["error"].(map[string]any)
		code, _ := detail["code"].(string)
		message, _ := detail["message"].(string)
		return code, message
	}

	status, answer, _ := send("POST", "/api/v1/apps", `{"name":"shop","server":"server-1"}`)
	first, _ := answer["password"].(string)
	if status != http.StatusCreated || len(first) < 16 || answer["protected"] != true || answer["instant_password"] != nil {
		t.Fatalf("create: %d %v", status, answer)
	}
	if _, _, body := send("GET", "/api/v1/apps/shop/instant", ""); strings.Contains(body, first) || strings.Contains(body, `"password"`) || strings.Contains(body, `"open"`) {
		t.Fatalf("the instant address shows a password: %s", body)
	}
	status, answer, _ = send("DELETE", "/api/v1/apps/shop/protection", "")
	if app, _ := answer["app"].(map[string]any); status != http.StatusOK || app["protected"] != false {
		t.Fatalf("off: %d %v", status, answer)
	}
	status, answer, _ = send("PUT", "/api/v1/apps/shop/protection", `{}`)
	if app, _ := answer["app"].(map[string]any); status != http.StatusOK || app["protected"] != true || answer["password"] == first || len(answer["password"].(string)) < 16 {
		t.Fatalf("on again: %d %v", status, answer)
	}
	status, answer, _ = send("PUT", "/api/v1/apps/shop/protection", `{"username":"ada","password":"my-own-password"}`)
	if status != http.StatusOK || answer["password"] != nil {
		t.Fatalf("own password: %d %v", status, answer)
	}
	// openapi_test.go keeps the document and the routes the same.
	if document := string(OpenAPIDocument); strings.Contains(document, "/instant/password") || strings.Contains(document, "instant_password") || strings.Contains(document, "instant_open") {
		t.Errorf("the API still has an instant password")
	}

	if status, answer, _ := send("PATCH", "/api/v1/apps/shop/env", `{"set":{"DATABASE_URL":"postgres://secret"}}`); status != http.StatusOK || len(answer["items"].([]any)) != 1 {
		t.Fatalf("env set: %d %v", status, answer)
	}
	status, answer, _ = send("PUT", "/api/v1/registries/ghcr.io", `{"username":"ada","password":"token"}`)
	items, _ := answer["items"].([]any)
	if status != http.StatusOK || len(items) != 1 {
		t.Fatalf("registry: %d %v", status, answer)
	}
	login := items[0].(map[string]any)
	if keepers := login["servers"].([]any); len(keepers) != 1 || keepers[0].(map[string]any)["server"] != "server-1" || len(login["missing_on"].([]any)) != 0 || login["password"] != nil {
		t.Fatalf("registry login = %v", login)
	}

	// A second server, offline: a secret for it waits for nobody.
	offline, _, err := servers.Create(actor, "server-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apps.Create(actor, "blog", offline.ID); err != nil {
		t.Fatal(err)
	}
	status, answer, _ = send("PATCH", "/api/v1/apps/blog/env", `{"set":{"KEY":"value"}}`)
	if code, message := errorOf(answer); status != http.StatusConflict || code != "server_offline" || message != "server-2 is offline. Try again when it is back." {
		t.Fatalf("env on an offline server: %d %v", status, answer)
	}
	status, answer, _ = send("PUT", "/api/v1/registries/ghcr.io", `{"username":"ada","password":"token"}`)
	if fields, _ := answer["error"].(map[string]any)["fields"].(map[string]any); status != http.StatusUnprocessableEntity || fields["server"] == nil {
		t.Fatalf("a login without a server of two: %d %v", status, answer)
	}
	status, answer, _ = send("GET", "/api/v1/integrations", "")
	tailscale, _ := answer["tailscale"].(map[string]any)
	if missing, _ := tailscale["missing_on"].([]any); status != http.StatusOK || len(missing) != 2 || len(tailscale["servers"].([]any)) != 0 {
		t.Fatalf("integrations: %d %v", status, answer)
	}
	status, answer, _ = send("PUT", "/api/v1/integrations", `{"tailscale_key":"tskey-auth-1","server":"server-1"}`)
	tailscale, _ = answer["tailscale"].(map[string]any)
	if keepers, _ := tailscale["servers"].([]any); status != http.StatusOK || len(keepers) != 1 || tailscale["missing_on"].([]any)[0] != "server-2" {
		t.Fatalf("tailscale on server-1: %d %v", status, answer)
	}
	if status, answer, _ := send("PUT", "/api/v1/integrations", `{"tailscale_key":"tskey-auth-1","server":"server-2"}`); status != http.StatusConflict {
		t.Fatalf("tailscale on an offline server: %d %v", status, answer)
	}
}
