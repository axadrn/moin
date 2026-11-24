package service

import (
	"errors"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
)

// signUpOpen signs a non-admin up once sign-up is open and makes their first
// workspace.
func signUpOpen(t *testing.T, beta *BetaService, environment authFlowEnvironment, address string) *model.Workspace {
	t.Helper()
	beta.open = true
	defer func() { beta.open = false }()
	user, err := environment.auth.SignInWithGitHub(GitHubAccount{ID: 9001, Login: "someone", Email: address}, "")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := environment.auth.CompleteOnboarding(user.ID, "Some One", "", true)
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func TestOpenSignupLetsAnyoneIn(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	beta := NewBetaService(true, nil, repository.NewWorkspaceInvitations(environment.database), environment.subscriptionRepository)
	environment.auth.SetBeta(beta)
	if _, err := environment.auth.SignInWithGitHub(GitHubAccount{ID: 1, Login: "anyone", Email: "anyone@example.com"}, ""); err != nil {
		t.Fatalf("open sign-up: err = %v", err)
	}
}

func TestClosedSignupIsPrivate(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	beta := NewBetaService(false, []string{"admin@example.com"}, repository.NewWorkspaceInvitations(environment.database), environment.subscriptionRepository)
	environment.auth.SetBeta(beta)

	if _, err := environment.auth.SignInWithGitHub(GitHubAccount{ID: 1, Login: "stranger", Email: "stranger@example.com"}, ""); !errors.Is(err, ErrPrivate) {
		t.Fatalf("a stranger: err = %v", err)
	}
	if _, err := environment.userRepository.ByEmail("stranger@example.com"); err == nil {
		t.Fatal("a stranger got an account")
	}

	if _, err := environment.auth.SignInWithGitHub(GitHubAccount{ID: 2, Login: "admin", Email: "Admin@Example.com"}, ""); err != nil {
		t.Fatalf("an admin: err = %v", err)
	}
	admin, err := environment.userRepository.ByEmail("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := environment.auth.CompleteOnboarding(admin.ID, "Ada Admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if subscription, _ := environment.subscriptionRepository.ByWorkspaceID(workspace.ID); !subscription.IsForeverFree() {
		t.Fatalf("the admin's workspace is not free forever: %+v", subscription)
	}
	_, member, err := environment.workspaceService.BySlugForUser(workspace.Slug, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.workspaceService.Invite(member, workspace, "mate@example.com", model.WorkspaceRoleMember, Source{Via: "web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.auth.SignInWithGitHub(GitHubAccount{ID: 3, Login: "mate", Email: "mate@example.com"}, ""); err != nil {
		t.Fatalf("an invited teammate: err = %v", err)
	}
	if _, err := environment.userRepository.ByEmail("mate@example.com"); err != nil {
		t.Fatalf("the invited teammate has no account: %v", err)
	}
	stranger := signUpOpen(t, beta, environment, "stranger2@example.com")
	if subscription, _ := environment.subscriptionRepository.ByWorkspaceID(stranger.ID); subscription.Status != model.SubscriptionStatusTrialing {
		t.Fatalf("someone who is not an admin got %+v, want the trial", subscription)
	}
}
