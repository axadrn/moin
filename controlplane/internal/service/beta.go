package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/access"
	"github.com/axadrn/ops/controlplane/internal/repository"
)

// ErrPrivate is the answer to a new address while sign-up is closed: only
// admins and invited people create accounts.
var ErrPrivate = errors.New("this control plane is private: ask its admin for an invitation")

// BetaService decides who may create an account. With sign-up closed, only
// admins and people invited to a workspace do. The first workspace of an
// admin is free forever.
type BetaService struct {
	open          bool
	admins        map[string]struct{}
	invitations   repository.WorkspaceInvitationRepository
	subscriptions repository.SubscriptionRepository
	now           func() time.Time
}

func NewBetaService(open bool, adminEmails []string, invitations repository.WorkspaceInvitationRepository, subscriptions repository.SubscriptionRepository) *BetaService {
	admins := make(map[string]struct{}, len(adminEmails))
	for _, address := range adminEmails {
		admins[strings.ToLower(strings.TrimSpace(address))] = struct{}{}
	}
	return &BetaService{open: open, admins: admins, invitations: invitations, subscriptions: subscriptions, now: time.Now}
}

// Admit lets a new address create an account, or says ErrPrivate.
func (s *BetaService) Admit(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if s.open {
		return nil
	}
	if _, ok := s.admins[email]; ok {
		return nil
	}
	invited, err := s.invitations.HasPending(email, s.now().UTC())
	if err != nil || invited {
		return err
	}
	return ErrPrivate
}

// Unlock makes the first workspace of an admin free forever. Everyone else
// keeps the trial their workspace started with.
func (s *BetaService) Unlock(email, workspaceID string) error {
	if _, admin := s.admins[strings.ToLower(strings.TrimSpace(email))]; !admin {
		return nil
	}
	if _, exists := access.PlanByID(access.PlanPro); !exists {
		return nil
	}
	subscription, err := s.subscriptions.ByWorkspaceID(workspaceID)
	if err != nil {
		return fmt.Errorf("admin workspace: %w", err)
	}
	if subscription.HasProviderSubscription() {
		return nil
	}
	subscription.MakeForeverFree(s.now().UTC())
	return s.subscriptions.Update(subscription)
}
