package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/config"
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

func TestHomeIsTheLandingPageOnlyWithBilling(t *testing.T) {
	telemetry := service.NewTelemetryService(repository.NewTelemetryRepository(testutil.NewDatabase(t)), "", "test", false)
	for _, test := range []struct {
		name     string
		billing  bool
		signedIn bool
		location string
	}{
		{name: "hosted", billing: true},
		{name: "self-hosted guest", location: "/auth"},
		{name: "self-hosted signed in", signedIn: true, location: "/app"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{Billing: test.billing})
			if test.signedIn {
				ctx = ctxkeys.WithUser(ctx, &model.User{ID: "user", Email: "user@example.com"})
			}
			recorder := httptest.NewRecorder()

			NewHomeHandler(telemetry, test.billing, t.TempDir()).HomePage(recorder, request.WithContext(ctx))

			if test.location == "" {
				if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "<html") {
					t.Fatalf("status = %d, want the landing page", recorder.Code)
				}
				return
			}
			if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != test.location {
				t.Fatalf("status = %d, location = %q, want %s", recorder.Code, recorder.Header().Get("Location"), test.location)
			}
		})
	}
}

func TestClosedSignupOnEachKindOfControlPlane(t *testing.T) {
	for _, test := range []struct {
		name    string
		billing bool
		open    bool
		email   string
		want    string
	}{
		{name: "self-hosted refuses a stranger", email: "stranger@example.com", want: "is private"},
		{name: "self-hosted lets the admin sign up", email: "admin@example.com", want: "/auth/magic-link-sent"},
		{name: "self-hosted lets an invited teammate sign up", email: "mate@example.com", want: "/auth/magic-link-sent"},
		{name: "self-hosted with SIGNUPS_OPEN=true is open", open: true, email: "stranger@example.com", want: "/auth/magic-link-sent"},
		{name: "hosted with sign-up closed refuses a stranger", billing: true, email: "stranger@example.com", want: "is private"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, workspaces, owner, mailer := invitationHandlerTestSetup(t)
			if err := workspaces.Invite(owner.WorkspaceMember, owner.Workspace, "mate@example.com", model.WorkspaceRoleMember, service.Source{Via: "web"}); err != nil {
				t.Fatalf("Invite() error = %v", err)
			}
			users := repository.NewUsers(database)
			email := service.NewEmailService(mailer, "https://app.example.com", "Test App")
			auth := service.NewAuthService(
				users, repository.NewTokenRepository(database), repository.NewSessionRepository(database), workspaces, email,
				false, time.Hour, time.Hour, time.Hour, time.Hour, time.Hour,
			)
			auth.SetBeta(service.NewBetaService(test.open, []string{"admin@example.com"}, repository.NewWorkspaceInvitationRepository(database), repository.NewSubscriptionRepository(database)))
			form := url.Values{"email": {test.email}}
			request := httptest.NewRequest(http.MethodPost, "/auth/magic-link", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request = request.WithContext(ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{Billing: test.billing}))
			recorder := httptest.NewRecorder()

			NewAuthHandler(auth, nil, workspaces, &config.Config{}).SendMagicLink(recorder, request)

			if strings.HasPrefix(test.want, "/") {
				if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != test.want {
					t.Fatalf("status = %d, location = %q, want %s", recorder.Code, recorder.Header().Get("Location"), test.want)
				}
				if _, err := users.ByEmail(test.email); err != nil {
					t.Fatalf("no account for %s: %v", test.email, err)
				}
				return
			}
			if !strings.Contains(recorder.Body.String(), test.want) {
				t.Fatalf("status = %d, the page does not say %q", recorder.Code, test.want)
			}
			if _, err := users.ByEmail(test.email); err == nil {
				t.Fatalf("%s got an account", test.email)
			}
			if links := strings.Contains(recorder.Body.String(), "/legal/"); links != test.billing {
				t.Fatalf("links to the legal pages of the hosted service: %v", links)
			}
		})
	}
}
