package service

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/google/uuid"
)

var (
	// ErrGitHubEmailTaken refuses a GitHub account whose email belongs to
	// an account it cannot sign in to: one with another GitHub account, or
	// one that never proved its address.
	ErrGitHubEmailTaken = errors.New("this email belongs to another account")
	// ErrGitHubNoEmail refuses a new account from GitHub without a
	// verified primary email.
	ErrGitHubNoEmail = errors.New("GitHub shared no verified primary email")
	// ErrGitHubTaken refuses to connect a GitHub account another user
	// signs in with.
	ErrGitHubTaken = errors.New("this GitHub account is connected to another account")
)

// SignInWithGitHub finds the user of a GitHub account, or makes one:
//
//  1. The user with this GitHub id. After the first sign-in only the id
//     counts, so a changed email on GitHub changes nothing.
//  2. Else the user whose verified email is GitHub's verified primary
//     email and who never had a GitHub account: it gets this one. After a
//     recovery key disconnected GitHub, only the settings connect it.
//  3. Else, when that email belongs to another account, nobody.
//  4. Else a new user, while sign-up is open to the email, or with the
//     token of a pending invitation. The invitation link proved its
//     mailbox, so the new account takes the invited email, whatever
//     GitHub says.
func (s *AuthService) SignInWithGitHub(account GitHubAccount, invitationToken string) (*model.User, error) {
	user, err := s.userRepository.ByGitHubID(account.ID)
	if err == nil {
		if user.GitHubLogin != account.Login {
			if err := s.userRepository.SetGitHubLogin(user.ID, account.Login); err != nil {
				return nil, fmt.Errorf("update the GitHub login: %w", err)
			}
			user.GitHubLogin = account.Login
		}
		return user, nil
	}
	if !errors.Is(err, repository.ErrUserNotFound) {
		return nil, err
	}

	email := strings.ToLower(strings.TrimSpace(account.Email))
	if email != "" {
		user, err = s.userRepository.ByEmail(email)
		switch {
		case err == nil:
			if user.EmailVerifiedAt == nil || user.GitHubID != nil || user.GitHubDisconnectedAt != nil {
				return nil, ErrGitHubEmailTaken
			}
			err = s.userRepository.ConnectGitHub(user.ID, account.ID, account.Login)
			if errors.Is(err, repository.ErrGitHubTaken) {
				return nil, ErrGitHubEmailTaken
			}
			if err != nil {
				return nil, err
			}
			user.GitHubID, user.GitHubLogin = &account.ID, account.Login
			slog.Info("github connected by its email", "user_id", user.ID)
			return user, nil
		case !errors.Is(err, repository.ErrUserNotFound):
			return nil, err
		}
	}

	invited := ""
	if invitationToken != "" {
		if invitation, err := s.workspaceService.WorkspaceInvitation(invitationToken); err == nil {
			invited = strings.ToLower(strings.TrimSpace(invitation.Email))
		}
	}
	switch {
	case invited != "":
		email = invited
	case email == "":
		return nil, ErrGitHubNoEmail
	default:
		if err := s.mayCreate(email); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	user = &model.User{
		ID:              uuid.New().String(),
		Email:           email,
		EmailVerifiedAt: &now,
		GitHubID:        &account.ID,
		GitHubLogin:     account.Login,
		CreatedAt:       now,
	}
	err = s.userRepository.Create(user)
	if errors.Is(err, repository.ErrDuplicateEmail) {
		return nil, ErrGitHubEmailTaken
	}
	if errors.Is(err, repository.ErrGitHubTaken) {
		return nil, ErrGitHubTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create the user: %w", err)
	}
	slog.Info("new user created with github", "user_id", user.ID)
	return user, nil
}

// ConnectGitHub gives a user without a GitHub account this one, to sign in
// with from now on.
func (s *AuthService) ConnectGitHub(userID string, account GitHubAccount) error {
	user, err := s.userRepository.ByID(userID)
	if err != nil {
		return err
	}
	if user.GitHubID != nil {
		return ErrGitHubTaken
	}
	err = s.userRepository.ConnectGitHub(userID, account.ID, account.Login)
	if errors.Is(err, repository.ErrGitHubTaken) {
		return ErrGitHubTaken
	}
	return err
}

// ErrGitHubOtherAccount refuses a confirmation with a GitHub account that is
// not the one the user signs in with.
var ErrGitHubOtherAccount = errors.New("this is not the GitHub account of this moin account")

// ConfirmWithGitHub notes that the person of session confirmed it is them
// with account, which must be the GitHub account they sign in with. The
// session stays, and so do the others: nobody signs out. For an account
// with a passkey it does not count (PasskeyService.Present).
func (s *AuthService) ConfirmWithGitHub(session *model.Session, account GitHubAccount) error {
	if session == nil || session.IsImpersonating() {
		return ErrImpersonating
	}
	user, err := s.userRepository.ByID(session.UserID)
	if err != nil {
		return err
	}
	if user.GitHubID == nil || *user.GitHubID != account.ID {
		return ErrGitHubOtherAccount
	}
	now := time.Now().UTC()
	if err := s.sessionRepository.Confirm(session.ID, model.ConfirmedWithGitHub, now); err != nil {
		return err
	}
	session.ConfirmedAt, session.ConfirmedWith = now, model.ConfirmedWithGitHub
	slog.Info("session confirmed with github", "user_id", user.ID)
	return nil
}
