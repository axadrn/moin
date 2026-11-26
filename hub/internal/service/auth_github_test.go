package service

import (
	"errors"
	"regexp"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
)

var octocat = GitHubAccount{ID: 583231, Login: "octocat", Name: "The Octocat", Email: "ada@example.com"}

// closedSignup closes sign-up like a self-hosted hub: only admins and
// invited people get an account.
func closedSignup(environment authFlowEnvironment) {
	environment.auth.SetBeta(NewBetaService(false, []string{"admin@example.com"}, repository.NewWorkspaceInvitations(environment.database), environment.subscriptionRepository))
}

func signInWithGitHub(t *testing.T, environment authFlowEnvironment, account GitHubAccount) *model.User {
	t.Helper()
	user, err := environment.auth.SignInWithGitHub(account, "")
	if err != nil {
		t.Fatalf("SignInWithGitHub(%+v) error = %v", account, err)
	}
	return user
}

// The first sign-in finds the account by GitHub's verified primary email
// and connects it; from then on the GitHub id alone counts, so a changed
// email or login on GitHub still signs in to the same account.
func TestGitHubSignInByEmailOnceThenByID(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	closedSignup(environment)
	ada := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})

	user := signInWithGitHub(t, environment, octocat)
	if user.ID != ada.User.ID || user.GitHubID == nil || *user.GitHubID != octocat.ID || user.GitHubLogin != "octocat" {
		t.Fatalf("first sign-in = %+v, want ada with the GitHub account", user)
	}
	stored, err := environment.userRepository.ByID(ada.User.ID)
	if err != nil || stored.GitHubID == nil || *stored.GitHubID != octocat.ID {
		t.Fatalf("the GitHub id was not stored: %+v, %v", stored, err)
	}

	moved := GitHubAccount{ID: octocat.ID, Login: "octocat-new", Email: "elsewhere@example.com"}
	user = signInWithGitHub(t, environment, moved)
	if user.ID != ada.User.ID || user.GitHubLogin != "octocat-new" {
		t.Fatalf("sign-in after a change on GitHub = %+v, want ada", user)
	}
	if stored, _ := environment.userRepository.ByID(ada.User.ID); stored.GitHubLogin != "octocat-new" || stored.Email != "ada@example.com" {
		t.Fatalf("stored = %q %q, want the new login and the old email", stored.GitHubLogin, stored.Email)
	}
}

// An account that never proved its address is not GitHub's to take, and
// does not get verified by trying.
func TestGitHubSignInRefusesAnUnverifiedAccount(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com"})

	if _, err := environment.auth.SignInWithGitHub(octocat, ""); !errors.Is(err, ErrGitHubEmailTaken) {
		t.Fatalf("error = %v, want ErrGitHubEmailTaken", err)
	}
	stored, err := environment.userRepository.ByID("ada")
	if err != nil || stored.EmailVerifiedAt != nil || stored.GitHubID != nil {
		t.Fatalf("the unverified account changed: %+v, %v", stored, err)
	}
}

// Only the primary email counts. An account under another address of the
// same person stays apart: with sign-up open GitHub makes a new one.
func TestGitHubSignInMatchesOnlyThePrimaryEmail(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "work", Email: "ada@work.example.com", Verified: true})

	user := signInWithGitHub(t, environment, octocat)
	if user.ID == "work" || user.Email != "ada@example.com" || user.EmailVerifiedAt == nil {
		t.Fatalf("user = %+v, want a new verified account for the primary email", user)
	}
	if stored, _ := environment.userRepository.ByID("work"); stored.GitHubID != nil {
		t.Fatal("the account of a secondary email got the GitHub account")
	}
}

// The email of an account that signs in with another GitHub account stays
// its own.
func TestGitHubSignInRefusesAnEmailWithAnotherGitHubAccount(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	signInWithGitHub(t, environment, octocat)

	other := GitHubAccount{ID: 1, Login: "mallory", Email: "ada@example.com"}
	if _, err := environment.auth.SignInWithGitHub(other, ""); !errors.Is(err, ErrGitHubEmailTaken) {
		t.Fatalf("error = %v, want ErrGitHubEmailTaken", err)
	}
}

// A new account needs open sign-up; GitHub without a verified primary
// email makes none.
func TestGitHubSignInNewAccounts(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	closedSignup(environment)
	if _, err := environment.auth.SignInWithGitHub(octocat, ""); !errors.Is(err, ErrPrivate) {
		t.Fatalf("closed sign-up: error = %v, want ErrPrivate", err)
	}
	if _, err := environment.userRepository.ByEmail("ada@example.com"); err == nil {
		t.Fatal("closed sign-up made an account")
	}

	environment.auth.SetBeta(nil)
	if _, err := environment.auth.SignInWithGitHub(GitHubAccount{ID: 7, Login: "nomail"}, ""); !errors.Is(err, ErrGitHubNoEmail) {
		t.Fatalf("no email: error = %v, want ErrGitHubNoEmail", err)
	}
	user := signInWithGitHub(t, environment, octocat)
	if user.Email != "ada@example.com" || user.EmailVerifiedAt == nil || user.GitHubLogin != "octocat" {
		t.Fatalf("new user = %+v", user)
	}
}

// An invitation link proves its mailbox: any GitHub account may take it,
// also on a closed hub, and the account gets the invited email.
func TestGitHubSignInAcceptsAnInvitationWithAnyAccount(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	closedSignup(environment)
	owner := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "owner", Email: "owner@example.com", Name: "Owner", Verified: true})
	if err := environment.workspaceService.Invite(owner.WorkspaceMember, owner.Workspace, "grace@example.com", model.WorkspaceRoleMember, Source{Via: ViaWeb}); err != nil {
		t.Fatal(err)
	}
	token := invitationToken(t, environment)

	stranger := GitHubAccount{ID: 42, Login: "hopper", Email: "hopper@elsewhere.example.com"}
	user, err := environment.auth.SignInWithGitHub(stranger, token)
	if err != nil {
		t.Fatalf("SignInWithGitHub() error = %v", err)
	}
	if user.Email != "grace@example.com" || user.EmailVerifiedAt == nil {
		t.Fatalf("user = %+v, want the invited email, verified", user)
	}
	if _, err := environment.workspaceService.AcceptInvitation(token, user, Source{Via: ViaWeb}); err != nil {
		t.Fatalf("AcceptInvitation() error = %v", err)
	}
	if _, err := environment.auth.SignInWithGitHub(GitHubAccount{ID: 43, Login: "late", Email: "late@example.com"}, token); !errors.Is(err, ErrPrivate) {
		t.Fatalf("a used invitation: error = %v, want ErrPrivate", err)
	}
}

// Connecting refuses a GitHub account another user signs in with, and a
// second one for a user who has one.
func TestConnectGitHubRefusesATakenAccount(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "bob", Email: "bob@example.com", Verified: true})
	signInWithGitHub(t, environment, octocat)

	if err := environment.auth.ConnectGitHub("bob", octocat); !errors.Is(err, ErrGitHubTaken) {
		t.Fatalf("taken: error = %v, want ErrGitHubTaken", err)
	}
	if err := environment.auth.ConnectGitHub("ada", GitHubAccount{ID: 2, Login: "second"}); !errors.Is(err, ErrGitHubTaken) {
		t.Fatalf("second account: error = %v, want ErrGitHubTaken", err)
	}
	if err := environment.auth.ConnectGitHub("bob", GitHubAccount{ID: 3, Login: "bob"}); err != nil {
		t.Fatalf("ConnectGitHub() error = %v", err)
	}
	user := signInWithGitHub(t, environment, GitHubAccount{ID: 3, Login: "bob", Email: "other@example.com"})
	if user.ID != "bob" {
		t.Fatalf("signed in as %s, want bob", user.ID)
	}
}

var invitationLink = regexp.MustCompile(`/workspaces/invitations/([A-Za-z0-9_-]+)`)

// invitationToken is the token of the last invitation mailed.
func invitationToken(t *testing.T, environment authFlowEnvironment) string {
	t.Helper()
	messages := environment.mailer.Messages()
	for index := len(messages) - 1; index >= 0; index-- {
		if match := invitationLink.FindStringSubmatch(messages[index].Text); match != nil {
			return match[1]
		}
	}
	t.Fatal("no invitation was mailed")
	return ""
}
