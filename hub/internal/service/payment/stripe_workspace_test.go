package payment

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
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
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptions(database), repository.NewAdmin(database))
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

// stripeSubscription is a subscription event as Stripe sends it, for one
// price of a product.
func stripeSubscription(workspaceID, status, product string, periodEnd int64, cancelAtPeriodEnd bool) []byte {
	payload, _ := json.Marshal(map[string]any{
		"id":                   "sub-1",
		"customer":             "stripe-customer-1",
		"status":               status,
		"cancel_at_period_end": cancelAtPeriodEnd,
		"metadata":             map[string]string{"workspace_id": workspaceID},
		"items": map[string]any{"data": []any{map[string]any{
			"current_period_end": periodEnd,
			"price":              map[string]any{"id": "price_old", "product": product, "unit_amount": 2900, "currency": "usd", "recurring": map[string]string{"interval": "month"}},
		}}},
	})
	return payload
}

// TestStripeUnknownProductKeepsThePlan: a subscription for a product moin
// does not sell, or with no product, neither opens nor changes a plan.
func TestStripeUnknownProductKeepsThePlan(t *testing.T) {
	database := testutil.NewDatabase(t)
	fixture := testutil.SeedUser(t, database, testutil.UserOptions{ID: "stripe-unknown-product"})
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptions(database), repository.NewAdmin(database))
	provider := &StripeProvider{cfg: &config.Config{StripeProductIDPro: "prod_hosted", StripePriceIDProMonthly: "price_new"}, subscriptionService: subscriptions}
	before, err := subscriptions.Subscription(fixture.Workspace.ID)
	if err != nil {
		t.Fatalf("Subscription() error = %v", err)
	}

	for _, product := range []string{"prod_other", ""} {
		if err := provider.handleSubscriptionCreated(stripeSubscription(fixture.Workspace.ID, "active", product, 1800000000, false)); err != nil {
			t.Fatalf("handleSubscriptionCreated(%q) error = %v", product, err)
		}
		after, err := subscriptions.Subscription(fixture.Workspace.ID)
		if err != nil {
			t.Fatalf("Subscription() error = %v", err)
		}
		if after.PlanID != before.PlanID || after.Status != before.Status || after.ProviderSubscriptionID != nil {
			t.Fatalf("product %q changed the subscription: %#v", product, after)
		}
	}
}

// TestStripeOldPriceOfTheProductKeepsThePlan: a subscription on a price
// moin no longer sells, of the hosted product, stays the paid plan through
// a renewal, cancel at period end and a failed payment.
func TestStripeOldPriceOfTheProductKeepsThePlan(t *testing.T) {
	database := testutil.NewDatabase(t)
	fixture := testutil.SeedUser(t, database, testutil.UserOptions{ID: "stripe-old-price"})
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptions(database), repository.NewAdmin(database))
	provider := &StripeProvider{cfg: &config.Config{StripeProductIDPro: "prod_hosted", StripePriceIDProMonthly: "price_new"}, subscriptionService: subscriptions}
	workspaceID := fixture.Workspace.ID

	if err := provider.handleSubscriptionCreated(stripeSubscription(workspaceID, "active", "prod_hosted", 1800000000, false)); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name, status string
		end          int64
		cancel       bool
		want         string
	}{
		{"renewal", "active", 1802592000, false, model.SubscriptionStatusActive},
		{"cancel at period end", "active", 1802592000, true, model.SubscriptionStatusCancelled},
		{"payment failed", "past_due", 1802592000, false, model.SubscriptionStatusPastDue},
	}
	for _, step := range steps {
		if err := provider.handleSubscriptionUpdated(stripeSubscription(workspaceID, step.status, "prod_hosted", step.end, step.cancel)); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		after, err := subscriptions.Subscription(workspaceID)
		if err != nil {
			t.Fatal(err)
		}
		if after.PlanID != model.SubscriptionPlanPro || after.Status != step.want || after.CurrentPeriodEnd == nil || after.CurrentPeriodEnd.Unix() != step.end {
			t.Fatalf("%s: plan %s, status %s, ends %v", step.name, after.PlanID, after.Status, after.CurrentPeriodEnd)
		}
	}
}
