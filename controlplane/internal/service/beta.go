package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/access"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/validation"
)

// ErrWaitlisted is the answer to a new address while sign-up is closed: it
// is on the waitlist now.
var ErrWaitlisted = errors.New("sign-up is closed for now: you are on the waitlist and hear from us when you are in")

// ErrPrivate is the answer to a new address on a control plane with sign-up
// closed and no waitlist, the way a self-hosted one runs.
var ErrPrivate = errors.New("this control plane is private: ask its admin for an invitation")

// BetaService closes sign-up: only admins, invited people and people let in
// from the waitlist create accounts. The hosted control plane puts everyone
// else on the waitlist; a self-hosted one has none and stays private.
type BetaService struct {
	open          bool
	waitlisting   bool
	admins        map[string]struct{}
	waitlist      repository.WaitlistRepository
	subscriptions repository.SubscriptionRepository
	email         *EmailService
	now           func() time.Time
}

func NewBetaService(open, waitlisting bool, adminEmails []string, waitlist repository.WaitlistRepository, subscriptions repository.SubscriptionRepository, email *EmailService) *BetaService {
	admins := make(map[string]struct{}, len(adminEmails))
	for _, address := range adminEmails {
		admins[strings.ToLower(strings.TrimSpace(address))] = struct{}{}
	}
	return &BetaService{open: open, waitlisting: waitlisting, admins: admins, waitlist: waitlist, subscriptions: subscriptions, email: email, now: time.Now}
}

// MayCreate reports whether a new account may be made for the address.
func (s *BetaService) MayCreate(email string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if s.open {
		return true, nil
	}
	if _, ok := s.admins[email]; ok {
		return true, nil
	}
	entry, err := s.waitlist.Get(email)
	if err != nil {
		return false, err
	}
	if entry != nil && entry.LetInAt != nil {
		return true, nil
	}
	return s.waitlist.HasPendingInvitation(email, s.now().UTC())
}

// Admit lets a new address create an account, or says why it may not: it
// joins the waitlist, or without one the control plane is private.
func (s *BetaService) Admit(email string) error {
	allowed, err := s.MayCreate(email)
	if err != nil || allowed {
		return err
	}
	if !s.waitlisting {
		return ErrPrivate
	}
	if err := s.Join(email); err != nil {
		return err
	}
	return ErrWaitlisted
}

// Join puts an address on the waitlist.
func (s *BetaService) Join(email string) error {
	return s.waitlist.Join(strings.ToLower(strings.TrimSpace(email)), s.now().UTC())
}

// LetIn lets an address in and tells it by email.
func (s *BetaService) LetIn(email string, foreverFree bool) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if err := validation.ValidateEmail(email); err != nil {
		return ErrInvalidEmail
	}
	if err := s.waitlist.LetIn(email, foreverFree, s.now().UTC()); err != nil {
		return err
	}
	return s.email.SendBetaInvite(email)
}

// Waitlist lists who waits and who is in, waiting first.
func (s *BetaService) Waitlist() ([]model.WaitlistEntry, error) {
	return s.waitlist.List()
}

// Unlock makes the first workspace of an admin, or of someone let in, a
// beta workspace: everything unlocked, without a payment.
func (s *BetaService) Unlock(email, workspaceID string) error {
	if _, exists := access.PlanByID(access.PlanPro); !exists {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if _, admin := s.admins[email]; !admin {
		entry, err := s.waitlist.Get(email)
		if err != nil || entry == nil || entry.LetInAt == nil {
			return err
		}
	}
	subscription, err := s.subscriptions.ByWorkspaceID(workspaceID)
	if err != nil {
		return fmt.Errorf("beta workspace: %w", err)
	}
	subscription.PlanID = access.PlanPro
	subscription.Status = access.StatusActive
	subscription.CurrentPeriodEnd = nil
	subscription.UpdatedAt = s.now().UTC()
	return s.subscriptions.Update(subscription)
}
