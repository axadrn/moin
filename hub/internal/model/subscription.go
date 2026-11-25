package model

import (
	"fmt"
	"time"

	"github.com/axadrn/moin/hub/internal/access"
)

type Subscription struct {
	ID                            string     `db:"id"`
	WorkspaceID                   string     `db:"workspace_id"`
	PlanID                        string     `db:"plan_id"`
	Status                        string     `db:"status"`
	Provider                      string     `db:"provider"`
	ProviderCustomerID            *string    `db:"provider_customer_id"`
	ProviderSubscriptionID        *string    `db:"provider_subscription_id"`
	ProviderSubscriptionCreatedAt *time.Time `db:"provider_subscription_created_at"`
	ProviderStateUpdatedAt        *time.Time `db:"provider_state_updated_at"`
	ProviderPaymentUpdatedAt      *time.Time `db:"provider_payment_updated_at"`
	CheckoutAttemptID             *string    `db:"checkout_attempt_id"`
	CheckoutProvider              string     `db:"checkout_provider"`
	CheckoutPlanID                string     `db:"checkout_plan_id"`
	CheckoutInterval              string     `db:"checkout_interval"`
	CheckoutURL                   *string    `db:"checkout_url"`
	CheckoutExpiresAt             *time.Time `db:"checkout_expires_at"`
	CheckoutPendingUntil          *time.Time `db:"checkout_pending_until"`
	CurrentPeriodEnd              *time.Time `db:"current_period_end"`
	CouponID                      *string    `db:"coupon_id"`
	Amount                        *int       `db:"amount"`
	Currency                      string     `db:"currency"`
	Interval                      *string    `db:"interval"`
	CreatedAt                     time.Time  `db:"created_at"`
	UpdatedAt                     time.Time  `db:"updated_at"`
}

const (
	SubscriptionStatusActive     = access.StatusActive
	SubscriptionStatusTrialing   = access.StatusTrialing
	SubscriptionStatusPastDue    = access.StatusPastDue
	SubscriptionStatusIncomplete = access.StatusIncomplete
	SubscriptionStatusInactive   = access.StatusInactive
	SubscriptionStatusCancelled  = access.StatusCancelled
	SubscriptionStatusBlocked    = access.StatusBlocked
)

const (
	ProviderPolar  = "polar"
	ProviderStripe = "stripe"
)

const (
	SubscriptionPlanFree = access.PlanFree
	SubscriptionPlanPro  = access.PlanPro
)

const (
	SubscriptionIntervalMonthly = "monthly"
	SubscriptionIntervalYearly  = "yearly"
)

func FormatPlanPrice(cents int) string {
	if cents%100 != 0 {
		return fmt.Sprintf("$%.2f", float64(cents)/100)
	}
	return fmt.Sprintf("$%d", cents/100)
}

func (s *Subscription) IsActive() bool {
	return s.IsActiveAt(time.Now())
}

func (s *Subscription) IsActiveAt(now time.Time) bool {
	currentAccess := s.AccessAt(now)
	return currentAccess.State == access.StateTrial ||
		currentAccess.State == access.StatePaid ||
		currentAccess.State == access.StateGrace ||
		(currentAccess.State == access.StateFree && s.PlanID == SubscriptionPlanFree)
}

func (s *Subscription) HasProviderSubscription() bool {
	return s.ProviderSubscriptionID != nil && *s.ProviderSubscriptionID != ""
}

// BlocksWorkspaceDeletion says whether the workspace must end its plan
// first. A trial that waits for its first server never blocks.
func (s *Subscription) BlocksWorkspaceDeletion(now time.Time) bool {
	paid := s.PlanID != SubscriptionPlanFree && s.IsActiveAt(now) && !s.TrialWaits()
	pendingCheckout := s.CheckoutPendingUntil != nil && now.Before(*s.CheckoutPendingUntil)
	return s.HasProviderSubscription() || paid || pendingCheckout
}

// IsForeverFree says whether the workspace has everything without paying:
// the paid plan, active, with no end and no subscription at the provider.
func (s *Subscription) IsForeverFree() bool {
	return s.PlanID == SubscriptionPlanPro && s.Status == SubscriptionStatusActive &&
		s.CurrentPeriodEnd == nil && !s.HasProviderSubscription()
}

// MakeForeverFree gives the workspace everything, forever, without paying.
func (s *Subscription) MakeForeverFree(now time.Time) {
	s.PlanID = SubscriptionPlanPro
	s.Status = SubscriptionStatusActive
	s.CurrentPeriodEnd = nil
	s.UpdatedAt = now
}

// StartOffer gives the workspace the paid plan for days, and a coupon for
// when it starts to pay.
func (s *Subscription) StartOffer(days int, couponID string, now time.Time) {
	end := now.AddDate(0, 0, days)
	s.PlanID = SubscriptionPlanPro
	s.Status = SubscriptionStatusTrialing
	s.CurrentPeriodEnd = &end
	s.CouponID = &couponID
	s.UpdatedAt = now
}

// EndForeverFree puts a forever free workspace back on what it would have
// without it: the trial, waiting for the first server, while the workspace
// has none; the free plan, read-only, once it has one.
func (s *Subscription) EndForeverFree(hasServer bool, now time.Time) {
	initial := access.Initial()
	if initial.Status == access.StatusTrialing && !hasServer {
		s.PlanID, s.Status = initial.PlanID, initial.Status
	} else {
		s.PlanID, s.Status = SubscriptionPlanFree, SubscriptionStatusActive
	}
	s.CurrentPeriodEnd = nil
	s.UpdatedAt = now
}

// TrialWaits says whether the trial has not started yet: it starts when
// the workspace's first server connects.
func (s *Subscription) TrialWaits() bool {
	return s.Status == SubscriptionStatusTrialing && s.CurrentPeriodEnd == nil
}

func (s *Subscription) ClearCheckout() {
	s.CheckoutAttemptID = nil
	s.CheckoutProvider = ""
	s.CheckoutPlanID = ""
	s.CheckoutInterval = ""
	s.CheckoutURL = nil
	s.CheckoutExpiresAt = nil
	s.CheckoutPendingUntil = nil
}

func (s *Subscription) FormatPrice() string {
	if s.Amount == nil || *s.Amount == 0 {
		return ""
	}

	currencySymbols := map[string]string{
		"usd": "$",
		"eur": "€",
		"gbp": "£",
	}

	amount := float64(*s.Amount) / 100.0
	symbol := currencySymbols[s.Currency]
	if symbol == "" {
		symbol = "$"
	}

	interval := "month"
	if s.Interval != nil && *s.Interval == SubscriptionIntervalYearly {
		interval = "year"
	}

	return fmt.Sprintf("%s%.0f/%s", symbol, amount, interval)
}

// HasFeature checks if the subscription has access to a specific feature
func (s *Subscription) HasFeature(feature access.Feature) bool {
	return s.Access().Has(feature)
}

func (s *Subscription) Access() access.Access { return s.AccessAt(time.Now()) }

func (s *Subscription) AccessAt(now time.Time) access.Access {
	statusChangedAt := s.ProviderStateUpdatedAt
	if statusChangedAt == nil && !s.UpdatedAt.IsZero() {
		statusChangedAt = &s.UpdatedAt
	}
	return access.Resolve(access.Subscription{
		PlanID:           s.PlanID,
		Status:           s.Status,
		CurrentPeriodEnd: s.CurrentPeriodEnd,
		StatusChangedAt:  statusChangedAt,
	}, now)
}
