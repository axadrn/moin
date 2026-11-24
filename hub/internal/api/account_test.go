package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/testutil"
)

func TestUpdateName(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		code   string
		want   string
	}{
		{name: "valid", body: `{"name":" Grace Hopper "}`, status: http.StatusOK, want: "Grace Hopper"},
		{name: "empty", body: `{"name":""}`, status: http.StatusUnprocessableEntity, code: "validation_failed"},
		{name: "control characters", body: `{"name":"Grace\u0007"}`, status: http.StatusUnprocessableEntity, code: "validation_failed"},
		{name: "unknown field", body: `{"display_name":"Grace"}`, status: http.StatusBadRequest, code: "invalid_body"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := newAPIEnvironment(t)
			user := seedVerifiedUser(t, environment, "namer", "Namer")
			token := environment.signIn(t, user.ID)

			recorder := environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/name", body: test.body, cookie: token})
			if test.code != "" {
				detail := expectError(t, recorder, test.status, test.code)
				if test.code == "validation_failed" && detail.Fields["name"] == "" {
					t.Fatalf("fields = %#v", detail.Fields)
				}
				return
			}
			expectStatus(t, recorder, test.status)
			response := decodeBody[api.UserResponse](t, recorder)
			if response.User.Name != test.want {
				t.Fatalf("name = %q, want %q", response.User.Name, test.want)
			}
		})
	}
}

func TestChangeEmail(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := seedVerifiedUser(t, environment, "changer", "Changer")
	seedVerifiedUser(t, environment, "taken", "Taken")
	token := environment.signIn(t, user.ID)

	same := environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/email", body: `{"email":"CHANGER@example.com"}`, cookie: token})
	expectError(t, same, http.StatusUnprocessableEntity, "validation_failed")
	invalid := environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/email", body: `{"email":"nope"}`, cookie: token})
	expectError(t, invalid, http.StatusUnprocessableEntity, "validation_failed")
	taken := environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/email", body: `{"email":"taken@example.com"}`, cookie: token})
	expectError(t, taken, http.StatusConflict, "email_taken")

	recorder := environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/email", body: `{"email":"changed@example.com"}`, cookie: token})
	expectStatus(t, recorder, http.StatusAccepted)
	verification := environment.mailedToken(t, "/auth/verify-email-change/")
	verified := environment.send(t, apiRequest{method: http.MethodPost, path: "/api/v1/auth/email-change/verify", body: `{"token":"` + verification + `"}`})
	expectStatus(t, verified, http.StatusOK)
	if decodeBody[api.UserResponse](t, verified).User.Email != "changed@example.com" {
		t.Fatalf("email change was not applied: %s", verified.Body.String())
	}
}

func TestChangeEmailIsRateLimitedPerRecipient(t *testing.T) {
	environment := newAPIEnvironment(t)
	for index, id := range []string{"first", "second", "third", "fourth"} {
		user := seedVerifiedUser(t, environment, id, "Limit")
		token := environment.signIn(t, user.ID)
		recorder := environment.send(t, apiRequest{method: http.MethodPatch, path: "/api/v1/account/email", body: `{"email":"wanted@example.com"}`, cookie: token})
		if index < 3 {
			expectStatus(t, recorder, http.StatusAccepted)
			continue
		}
		expectError(t, recorder, http.StatusTooManyRequests, "rate_limited")
	}
}

func TestDeleteAccount(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := seedVerifiedUser(t, environment, "leaving", "Leaving")
	coOwner := seedVerifiedUser(t, environment, "coowner", "Co Owner")
	_, err := environment.database.Exec(
		`INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at) VALUES ($1, $2, $3, $4, $5)`,
		"shared-coowner", "workspace-leaving", coOwner.ID, "owner", time.Now(),
	)
	if err != nil {
		t.Fatalf("add co-owner: %v", err)
	}
	token := environment.signIn(t, user.ID)

	wrong := environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account", body: `{"confirm_email":"other@example.com"}`, cookie: token})
	detail := expectError(t, wrong, http.StatusUnprocessableEntity, "validation_failed")
	if detail.Fields["confirm_email"] == "" {
		t.Fatalf("fields = %#v", detail.Fields)
	}

	recorder := environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account", body: `{"confirm_email":"LEAVING@example.com"}`, cookie: token})
	expectStatus(t, recorder, http.StatusNoContent)
	expectError(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: token}), http.StatusUnauthorized, "unauthenticated")
	_, err = environment.users.ByID(user.ID)
	if err == nil {
		t.Fatal("account still exists")
	}
}

func TestDeleteAccountRefusesSoleWorkspaceOwner(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedVerifiedUser(t, environment, "owner", "Owner")
	token := environment.signIn(t, owner.ID)

	recorder := environment.send(t, apiRequest{method: http.MethodDelete, path: "/api/v1/account", body: `{"confirm_email":"owner@example.com"}`, cookie: token})
	expectError(t, recorder, http.StatusConflict, "sole_workspace_owner")
	expectStatus(t, environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: token}), http.StatusOK)
}

func TestAccountRoutesRequireOnboarding(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "unnamed", Email: "unnamed@example.com", Verified: true}).User
	token := environment.signIn(t, user.ID)

	routes := []apiRequest{
		{method: http.MethodPatch, path: "/api/v1/account/name", body: `{"name":"Named"}`},
		{method: http.MethodPatch, path: "/api/v1/account/email", body: `{"email":"named@example.com"}`},
		{method: http.MethodDelete, path: "/api/v1/account", body: `{"confirm_email":"unnamed@example.com"}`},
		{method: http.MethodGet, path: "/api/v1/account/sessions"},
		{method: http.MethodDelete, path: "/api/v1/account/sessions/any"},
		{method: http.MethodDelete, path: "/api/v1/account/sessions"},
		{method: http.MethodPost, path: "/api/v1/account/avatar"},
		{method: http.MethodDelete, path: "/api/v1/account/avatar"},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			anonymous := route
			expectError(t, environment.send(t, anonymous), http.StatusUnauthorized, "unauthenticated")
			route.cookie = token
			expectError(t, environment.send(t, route), http.StatusForbidden, "onboarding_required")
		})
	}
}

// TestEmailAlerts: the account says the alerts are on until the user turns
// them off.
func TestEmailAlerts(t *testing.T) {
	environment := newAPIEnvironment(t)
	user := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Name: "Ada", Verified: true})
	token := environment.signIn(t, user.User.ID)

	account := environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: token})
	if account.Code != http.StatusOK || !strings.Contains(account.Body.String(), `"email_alerts":true`) {
		t.Fatalf("GET account = %d %s", account.Code, account.Body)
	}
	off := environment.send(t, apiRequest{method: http.MethodPut, path: "/api/v1/account/email-alerts", body: `{"on":false}`, cookie: token})
	if off.Code != http.StatusOK || off.Body.String() != "{\"on\":false}\n" {
		t.Fatalf("PUT email-alerts = %d %s", off.Code, off.Body)
	}
	account = environment.send(t, apiRequest{method: http.MethodGet, path: "/api/v1/account", cookie: token})
	if !strings.Contains(account.Body.String(), `"email_alerts":false`) {
		t.Fatalf("GET account after off = %s", account.Body)
	}
	invalid := environment.send(t, apiRequest{method: http.MethodPut, path: "/api/v1/account/email-alerts", body: `{}`, cookie: token})
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT email-alerts {} = %d %s", invalid.Code, invalid.Body)
	}
}
