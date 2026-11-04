package payment

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/config"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

func TestStripeStatusPreservesVisibleBillingStates(t *testing.T) {
	provider := &StripeProvider{}
	tests := map[string]string{
		"active": model.SubscriptionStatusActive, "trialing": model.SubscriptionStatusTrialing,
		"past_due": model.SubscriptionStatusPastDue, "canceled": model.SubscriptionStatusCancelled,
		"unpaid": model.SubscriptionStatusInactive, "incomplete_expired": model.SubscriptionStatusInactive,
	}
	for input, want := range tests {
		if got := provider.mapStripeStatus(input); got != want {
			t.Fatalf("mapStripeStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStripeRejectsInvalidWebhookSignature(t *testing.T) {
	provider := &StripeProvider{cfg: &config.Config{StripeWebhookSecret: "webhook-secret"}}
	err := provider.HandleWebhook([]byte(`{"id":"event"}`), http.Header{"Stripe-Signature": {"invalid"}})
	if err == nil || !strings.Contains(err.Error(), "verify webhook signature") {
		t.Fatalf("HandleWebhook() error = %v, want signature verification error", err)
	}
}

func TestStripeWebhookBindsCustomerToWorkspace(t *testing.T) {
	database := testutil.NewDatabase(t)
	fixture := testutil.SeedUser(t, database, testutil.UserOptions{ID: "stripe-workspace"})
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptionRepository(database), repository.NewAdminRepository(database))
	provider := &StripeProvider{cfg: &config.Config{}, subscriptionService: subscriptions}
	payload, _ := json.Marshal(map[string]any{
		"id":       "checkout-1",
		"customer": "stripe-customer-1",
		"metadata": map[string]string{"workspace_id": fixture.Workspace.ID},
	})

	if err := provider.handleCheckoutSessionCompleted(payload); err != nil {
		t.Fatalf("handleCheckoutSessionCompleted() error = %v", err)
	}
	subscription, err := subscriptions.Subscription(fixture.Workspace.ID)
	if err != nil {
		t.Fatalf("Subscription() error = %v", err)
	}
	if subscription.Provider != model.ProviderStripe || stringValue(subscription.ProviderCustomerID) != "stripe-customer-1" {
		t.Fatalf("workspace subscription = %#v", subscription)
	}
}

func TestStripePeriodEndComesFromTheItemSinceBasil(t *testing.T) {
	if end := stripePeriodEnd(0, 1800000000); end != 1800000000 {
		t.Fatalf("basil payload: end = %d, want the item's", end)
	}
	if end := stripePeriodEnd(1700000000, 1800000000); end != 1700000000 {
		t.Fatalf("older payload: end = %d, want the subscription's", end)
	}
}

// TestStripeUnknownPriceKeepsThePlan: a subscription for a price moin does
// not sell, or with no price, neither opens nor changes a plan.
func TestStripeUnknownPriceKeepsThePlan(t *testing.T) {
	database := testutil.NewDatabase(t)
	fixture := testutil.SeedUser(t, database, testutil.UserOptions{ID: "stripe-unknown-price"})
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptionRepository(database), repository.NewAdminRepository(database))
	provider := &StripeProvider{cfg: &config.Config{StripePriceIDProMonthly: "price_pro"}, subscriptionService: subscriptions}
	before, err := subscriptions.Subscription(fixture.Workspace.ID)
	if err != nil {
		t.Fatalf("Subscription() error = %v", err)
	}

	for _, price := range []string{"price_other", ""} {
		payload, _ := json.Marshal(map[string]any{
			"id":       "sub-unknown",
			"customer": "stripe-customer-2",
			"status":   "active",
			"metadata": map[string]string{"workspace_id": fixture.Workspace.ID},
			"items": map[string]any{"data": []any{map[string]any{
				"current_period_end": 1800000000,
				"price":              map[string]any{"id": price, "unit_amount": 100, "currency": "usd", "recurring": map[string]string{"interval": "month"}},
			}}},
		})
		if err := provider.handleSubscriptionCreated(payload); err != nil {
			t.Fatalf("handleSubscriptionCreated(%q) error = %v", price, err)
		}
		after, err := subscriptions.Subscription(fixture.Workspace.ID)
		if err != nil {
			t.Fatalf("Subscription() error = %v", err)
		}
		if after.PlanID != before.PlanID || after.Status != before.Status || after.ProviderSubscriptionID != nil {
			t.Fatalf("price %q changed the subscription: %#v", price, after)
		}
	}
}
