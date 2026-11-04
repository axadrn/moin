package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/validation"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCurrentPassword = errors.New("current password is incorrect")

func (s *UserService) ByID(id string) (*model.User, error) {
	user, err := s.userRepository.ByID(id)
	if err != nil {
		return nil, err
	}

	return s.decorateUser(user)
}

func (s *UserService) UpdatePassword(userID, currentPassword, newPassword string) error {
	user, err := s.userRepository.ByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	if !user.HasPassword() {
		return fmt.Errorf("passwordless accounts cannot update password. Please set a password first")
	}

	err = bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(currentPassword))
	if err != nil {
		return ErrInvalidCurrentPassword
	}

	// Validate new password
	err = validation.ValidatePassword(newPassword)
	if err != nil {
		return err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	hashStr := string(hashedPassword)
	user.PasswordHash = &hashStr

	err = s.userRepository.Update(user)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	return nil
}

func (s *UserService) UpdateName(userID, name string) (*model.User, error) {
	if err := validation.ValidateName(name); err != nil {
		return nil, err
	}
	if err := s.userRepository.UpdateName(userID, strings.TrimSpace(name), time.Now()); err != nil {
		return nil, err
	}
	return s.ByID(userID)
}

// ErrView is a page or a view the Apps and Servers pages do not have.
var ErrView = errors.New("view must be list or grid, for apps or servers")

// SetView remembers how the user sees the "apps" or the "servers" page:
// model.ViewList or model.ViewGrid.
func (s *UserService) SetView(userID, page, view string) error {
	if (page != "apps" && page != "servers") || (view != model.ViewList && view != model.ViewGrid) {
		return ErrView
	}
	return s.userRepository.SetView(userID, page, view)
}

// SetEmailAlerts turns the email alerts of the user on or off: a mail
// when a site goes down or comes back, a server goes offline or comes back,
// or a deploy fails, in the workspaces they own or run as admin.
func (s *UserService) SetEmailAlerts(userID string, on bool) error {
	return s.userRepository.SetEmailAlerts(userID, on)
}

func (s *UserService) DeleteAccount(userID string) error {
	if err := s.validateAccountDeletion(userID); err != nil {
		return err
	}

	user, err := s.userRepository.ByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	name := "User"
	if user.Name != "" {
		name = user.Name
	}

	return s.finishAccountDeletion(user, name)
}
