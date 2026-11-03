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
	activity      repository.WorkspaceAuditRepository
	now           func() time.Time
}

func NewBetaService(open, waitlisting bool, adminEmails []string, waitlist repository.WaitlistRepository, subscriptions repository.SubscriptionRepository, email *EmailService) *BetaService {
	admins := make(map[string]struct{}, len(adminEmails))
	for _, address := range adminEmails {
		admins[strings.ToLower(strings.TrimSpace(address))] = struct{}{}
	}
	return &BetaService{open: open, waitlisting: waitlisting, admins: admins, waitlist: waitlist, subscriptions: subscriptions, email: email, now: time.Now}
}

// UseActivity writes plan changes it makes to the activity of the workspace.
func (s *BetaService) UseActivity(activity repository.WorkspaceAuditRepository) {
	s.activity = activity
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

// ErrPaying means the workspace pays through the payment provider: end the
// subscription before making it free forever.
var ErrPaying = errors.New("this workspace pays for its plan: cancel the subscription before making it free forever")

// LetIn lets an address in, or changes whether it is free forever. A new
// address gets the sign-in email. For an address with an account, the first
// workspace it created changes right away: free forever, or back to its
// trial or the free plan.
func (s *BetaService) LetIn(email string, foreverFree bool) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if err := validation.ValidateEmail(email); err != nil {
		return ErrInvalidEmail
	}
	before, err := s.waitlist.Get(email)
	if err != nil {
		return err
	}
	workspace, err := s.waitlist.FirstWorkspace(email)
	if err != nil {
		return err
	}
	if workspace != nil {
		if err := s.setForeverFree(workspace, foreverFree); err != nil {
			return err
		}
	}
	if err := s.waitlist.LetIn(email, foreverFree, s.now().UTC()); err != nil {
		return err
	}
	if workspace != nil || (before != nil && before.LetInAt != nil) {
		return nil
	}
	return s.email.SendBetaInvite(email)
}

// Waitlist lists who waits and who is in, waiting first, with the plan of
// everyone who is in.
func (s *BetaService) Waitlist() ([]model.WaitlistEntry, error) {
	entries, err := s.waitlist.List()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].LetInAt == nil {
			continue
		}
		if err := s.describePlan(&entries[i]); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (s *BetaService) describePlan(entry *model.WaitlistEntry) error {
	entry.Plan = model.WaitlistNoAccount
	workspace, err := s.waitlist.FirstWorkspace(entry.Email)
	if err != nil || workspace == nil {
		return err
	}
	subscription, err := s.subscriptions.ByWorkspaceID(workspace.ID)
	if err != nil {
		return fmt.Errorf("plan of %s: %w", entry.Email, err)
	}
	current := subscription.AccessAt(s.now())
	switch {
	case isForeverFree(subscription):
		entry.Plan = model.WaitlistFreeForever
	case subscription.HasProviderSubscription() && current.State != access.StateFree:
		entry.Plan = model.WaitlistPaying
	case current.State == access.StateTrial:
		entry.Plan = model.WaitlistTrial
		entry.TrialEndsAt = current.TrialEndsAt
	default:
		entry.Plan = model.WaitlistTrialEnded
	}
	return nil
}

// Unlock makes the first workspace of an admin, or of someone let in as
// free forever, free forever. Everyone else keeps the trial their workspace
// started with.
func (s *BetaService) Unlock(email, workspaceID string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if _, admin := s.admins[email]; !admin {
		entry, err := s.waitlist.Get(email)
		if err != nil || entry == nil || entry.LetInAt == nil || !entry.ForeverFree {
			return err
		}
	}
	subscription, err := s.subscriptions.ByWorkspaceID(workspaceID)
	if err != nil {
		return fmt.Errorf("beta workspace: %w", err)
	}
	if subscription.HasProviderSubscription() {
		return nil
	}
	return s.update(subscription, access.PlanPro, access.StatusActive, nil)
}

// setForeverFree makes a workspace free forever, or puts a free forever one
// back on the plan it would have without it: the rest of its trial, or the
// free plan once the trial is over. A paying workspace is left alone.
func (s *BetaService) setForeverFree(workspace *model.Workspace, on bool) error {
	if _, exists := access.PlanByID(access.PlanPro); !exists {
		return nil
	}
	subscription, err := s.subscriptions.ByWorkspaceID(workspace.ID)
	if err != nil {
		return fmt.Errorf("workspace plan: %w", err)
	}
	if on {
		if isForeverFree(subscription) {
			return nil
		}
		if subscription.HasProviderSubscription() {
			return ErrPaying
		}
		return s.update(subscription, access.PlanPro, access.StatusActive, nil)
	}
	if !isForeverFree(subscription) {
		return nil
	}
	initial := access.Initial(workspace.CreatedAt)
	if initial.Status == access.StatusTrialing && initial.CurrentPeriodEnd != nil && s.now().Before(*initial.CurrentPeriodEnd) {
		return s.update(subscription, initial.PlanID, initial.Status, initial.CurrentPeriodEnd)
	}
	return s.update(subscription, access.PlanFree, access.StatusActive, nil)
}

// isForeverFree says whether a workspace has everything without paying: on
// the paid plan, active, with no end and no subscription at the provider.
func isForeverFree(subscription *model.Subscription) bool {
	return subscription.PlanID == access.PlanPro && subscription.Status == access.StatusActive &&
		subscription.CurrentPeriodEnd == nil && !subscription.HasProviderSubscription()
}

func (s *BetaService) update(subscription *model.Subscription, planID, status string, periodEnd *time.Time) error {
	before := *subscription
	subscription.PlanID = planID
	subscription.Status = status
	subscription.CurrentPeriodEnd = periodEnd
	subscription.UpdatedAt = s.now().UTC()
	if err := s.subscriptions.Update(subscription); err != nil {
		return err
	}
	if before.PlanID != planID || before.Status != status {
		recordActivity(s.activity, s.now(), System(subscription.WorkspaceID), "billing.plan_changed", "subscription", subscription.WorkspaceID, map[string]string{
			"from": before.PlanID, "to": planID, "status_from": before.Status, "status": status, "provider": "ops",
		})
	}
	return nil
}
