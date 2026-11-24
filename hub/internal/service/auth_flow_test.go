package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/jmoiron/sqlx"
)

type authFlowEnvironment struct {
	database               *sqlx.DB
	mailer                 *testutil.MemoryMailer
	auth                   *AuthService
	userRepository         *repository.Users
	subscriptionRepository *repository.Subscriptions
	workspaceService       *WorkspaceService
}

func newAuthFlowEnvironment(t *testing.T) authFlowEnvironment {
	t.Helper()
	database := testutil.NewDatabase(t)
	userRepository := repository.NewUsers(database)
	tokenRepository := repository.NewTokens(database)
	sessionRepository := repository.NewSessions(database)
	subscriptionRepository := repository.NewSubscriptions(database)
	subscriptionService := NewSubscriptionService(subscriptionRepository, repository.NewAdmin(database))
	mailer := testutil.NewMemoryMailer()
	emailService := NewEmailService(mailer, "http://localhost:8090", "Test App")
	workspaceService := NewWorkspaceService(
		repository.NewWorkspaces(database),
		repository.NewWorkspaceMembers(database),
		repository.NewWorkspaceInvitations(database),
		repository.NewActivity(database),
		userRepository,
		subscriptionService,
		emailService,
	)
	auth := NewAuthService(
		userRepository,
		tokenRepository,
		sessionRepository,
		workspaceService,
		emailService,
		false,
		7*24*time.Hour,
		24*time.Hour,
		24*time.Hour,
	)
	return authFlowEnvironment{
		database:               database,
		mailer:                 mailer,
		auth:                   auth,
		userRepository:         userRepository,
		subscriptionRepository: subscriptionRepository,
		workspaceService:       workspaceService,
	}
}

// newUser makes the account of email, without a name yet.
func (e authFlowEnvironment) newUser(t *testing.T, email string) *model.User {
	t.Helper()
	user, err := e.auth.EnsureUser(email)
	if err != nil {
		t.Fatalf("EnsureUser(%q) error = %v", email, err)
	}
	return user
}

func TestOnboardingCreatesWorkspaceExceptForInvitedSignup(t *testing.T) {
	t.Run("normal signup", func(t *testing.T) {
		environment := newAuthFlowEnvironment(t)
		user := environment.newUser(t, "normal@example.com")
		workspace, err := environment.auth.CompleteOnboarding(user.ID, "Normal User", "", true)
		if err != nil {
			t.Fatalf("CompleteOnboarding() error = %v", err)
		}
		if workspace == nil {
			t.Fatal("normal onboarding did not create a workspace")
		}
		members, err := environment.workspaceService.ListForUser(user.ID)
		if err != nil || len(members) != 1 || members[0].Role != model.WorkspaceRoleOwner {
			t.Fatalf("members = %#v, error = %v", members, err)
		}
		subscription, err := environment.subscriptionRepository.ByWorkspaceID(workspace.ID)
		if err != nil {
			t.Fatalf("workspace subscription lookup: %v", err)
		}
		if subscription.Provider != "" {
			t.Fatalf("free workspace provider = %q, want empty", subscription.Provider)
		}
	})

	t.Run("invited signup", func(t *testing.T) {
		environment := newAuthFlowEnvironment(t)
		user := environment.newUser(t, "invited@example.com")
		workspace, err := environment.auth.CompleteOnboarding(user.ID, "Invited User", "", false)
		if err != nil {
			t.Fatalf("CompleteOnboarding() error = %v", err)
		}
		if workspace != nil {
			t.Fatalf("invited onboarding workspace = %#v, want nil", workspace)
		}
		members, err := environment.workspaceService.ListForUser(user.ID)
		if err != nil || len(members) != 0 {
			t.Fatalf("invited onboarding members = %#v, error = %v", members, err)
		}
	})

	t.Run("long valid name uses safe workspace fallback", func(t *testing.T) {
		environment := newAuthFlowEnvironment(t)
		user := environment.newUser(t, "long-name@example.com")
		name := strings.Repeat("A", 100)
		workspace, err := environment.auth.CompleteOnboarding(user.ID, name, "", true)
		if err != nil {
			t.Fatalf("CompleteOnboarding() error = %v", err)
		}
		if workspace.Name != "My Workspace" {
			t.Fatalf("workspace name = %q, want My Workspace", workspace.Name)
		}
		updated, err := environment.userRepository.ByID(user.ID)
		if err != nil || updated.Name != name {
			t.Fatalf("user name = %q, error = %v", updated.Name, err)
		}
	})

	t.Run("chosen workspace name", func(t *testing.T) {
		environment := newAuthFlowEnvironment(t)
		user := environment.newUser(t, "chosen@example.com")
		workspace, err := environment.auth.CompleteOnboarding(user.ID, "Grace Hopper", "  Acme Labs ", true)
		if err != nil {
			t.Fatalf("CompleteOnboarding() error = %v", err)
		}
		if workspace.Name != "Acme Labs" || workspace.Slug != "acme-labs" {
			t.Fatalf("workspace = %q %q, want Acme Labs acme-labs", workspace.Name, workspace.Slug)
		}
	})

	t.Run("default workspace name keeps the short slug", func(t *testing.T) {
		environment := newAuthFlowEnvironment(t)
		user := environment.newUser(t, "default@example.com")
		workspace, err := environment.auth.CompleteOnboarding(user.ID, "Ada Lovelace", "Ada Lovelace's Workspace", true)
		if err != nil {
			t.Fatalf("CompleteOnboarding() error = %v", err)
		}
		if workspace.Name != "Ada Lovelace's Workspace" || workspace.Slug != "ada-lovelace" {
			t.Fatalf("workspace = %q %q", workspace.Name, workspace.Slug)
		}
	})

	t.Run("invalid workspace name saves nothing", func(t *testing.T) {
		environment := newAuthFlowEnvironment(t)
		user := environment.newUser(t, "short@example.com")
		if _, err := environment.auth.CompleteOnboarding(user.ID, "Grace", "x", true); !errors.Is(err, ErrInvalidWorkspace) {
			t.Fatalf("CompleteOnboarding() error = %v, want ErrInvalidWorkspace", err)
		}
		updated, err := environment.userRepository.ByID(user.ID)
		if err != nil || updated.Name != "" {
			t.Fatalf("user name = %q, error = %v", updated.Name, err)
		}
	})

	t.Run("control characters are rejected before trimming", func(t *testing.T) {
		environment := newAuthFlowEnvironment(t)
		user := environment.newUser(t, "control-name@example.com")
		for _, name := range []string{"Jane\n", "\tJane", "Jane\r"} {
			if _, err := environment.auth.CompleteOnboarding(user.ID, name, "", true); err == nil {
				t.Fatalf("CompleteOnboarding(%q) error = nil", name)
			}
		}
		updated, err := environment.userRepository.ByID(user.ID)
		if err != nil || updated.Name != "" {
			t.Fatalf("user name = %q, error = %v", updated.Name, err)
		}
	})
}
