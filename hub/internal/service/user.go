package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/validation"
)

func (s *UserService) ByID(id string) (*model.User, error) {
	user, err := s.userRepository.ByID(id)
	if err != nil {
		return nil, err
	}

	return s.decorateUser(user)
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
