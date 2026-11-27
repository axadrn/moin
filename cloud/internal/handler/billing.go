package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/stripe"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/build"
)

// Subscribe sends the browser to Stripe's checkout, for a monthly or a
// yearly subscription. A paid workspace goes to the portal instead.
func (c *Cloud) Subscribe(w http.ResponseWriter, r *http.Request) {
	space, ok := c.signedIn(r)
	if !ok || !c.sameOrigin(r) {
		http.Error(w, "sign in first", http.StatusForbidden)
		return
	}
	if c.Stripe == nil {
		http.Error(w, "Nobody can subscribe here: this site has no Stripe account.", http.StatusServiceUnavailable)
		return
	}
	if space.Status == "paid" {
		http.Redirect(w, r, "/cloud/billing", http.StatusSeeOther)
		return
	}
	interval := r.PostFormValue("interval")
	if c.Stripe.Price(interval) == "" {
		http.Error(w, "Pay monthly or yearly.", http.StatusBadRequest)
		return
	}
	address, err := c.Stripe.Checkout(r.Context(), stripe.Checkout{
		Workspace: space.ID, Mail: space.Mail, Customer: space.StripeCustomer,
		Interval: interval, Coupon: space.Coupon,
		SuccessURL: c.appURL + "/cloud", CancelURL: c.appURL + "/cloud",
	})
	if err != nil {
		slog.Error("checkout not started", "workspace", space.ID, "error", err)
		http.Error(w, "The checkout did not start. Try again in a minute.", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, address, http.StatusSeeOther)
}

// Billing sends the browser to Stripe's customer portal: invoices, the
// card, cancel. Without a customer in Stripe there is nothing to show
// yet, so it goes back to /cloud.
func (c *Cloud) Billing(w http.ResponseWriter, r *http.Request) {
	space, ok := c.signedIn(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/cloud/billing", http.StatusSeeOther)
		return
	}
	if c.Stripe == nil || space.StripeCustomer == "" {
		http.Redirect(w, r, "/cloud", http.StatusSeeOther)
		return
	}
	address, err := c.Stripe.Portal(r.Context(), space.StripeCustomer, c.appURL+"/cloud")
	if err != nil {
		slog.Error("portal not opened", "workspace", space.ID, "error", err)
		http.Error(w, "Billing did not open. Try again in a minute.", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, address, http.StatusSeeOther)
}

// StripeWebhook takes what Stripe tells: a checkout is done, a
// subscription changed or ended. Each event only names the subscription;
// moin cloud reads it from Stripe, so a late or doubled event changes
// nothing. An answer that is not 2xx makes Stripe send it again.
func (c *Cloud) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	if c.Stripe == nil {
		http.NotFound(w, r)
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		return
	}
	event, err := c.Stripe.Event(payload, r.Header.Get("Stripe-Signature"), time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The object is a checkout or a subscription. Of a checkout, the
	// subscription it made counts, and the workspace that started it.
	var object struct {
		ID                string `json:"id"`
		Subscription      string `json:"subscription"`
		ClientReferenceID string `json:"client_reference_id"`
	}
	if err := json.Unmarshal(event.Data.Object, &object); err != nil {
		http.Error(w, "not an event", http.StatusBadRequest)
		return
	}
	var subscription, workspaceID string
	switch event.Type {
	case "checkout.session.completed":
		subscription, workspaceID = object.Subscription, object.ClientReferenceID
	case "customer.subscription.updated", "customer.subscription.deleted":
		subscription = object.ID
	}
	if subscription == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	sub, err := c.Stripe.Subscription(r.Context(), subscription)
	if err == nil {
		err = c.applySubscription(r.Context(), sub, workspaceID)
	}
	if err != nil {
		slog.Error("stripe event not applied, Stripe sends it again", "event", event.ID, "type", event.Type, "error", err)
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// applySubscription sets the plan of the workspace a subscription
// belongs to. That is the workspace of its customer first, as an
// imported one has, else the one that started the checkout.
func (c *Cloud) applySubscription(ctx context.Context, sub stripe.Subscription, workspaceID string) error {
	space, err := c.store.ByStripeCustomer(sub.Customer)
	if notFound(err) {
		if workspaceID == "" {
			workspaceID = sub.Metadata["workspace_id"]
		}
		space, err = c.store.Workspace(workspaceID)
	}
	if notFound(err) {
		slog.Warn("stripe subscription of no workspace", "subscription", sub.ID, "customer", sub.Customer)
		return nil
	}
	if err != nil {
		return err
	}
	if !c.Stripe.Sells(sub) {
		slog.Error("stripe subscription of another product, plan unchanged", "workspace", space.ID, "subscription", sub.ID)
		return nil
	}
	ours := space.StripeSubscription == "" || space.StripeSubscription == sub.ID
	status := space.Status
	switch sub.Status {
	case "active", "trialing", "past_due":
		// past_due: Stripe tries the card again for a while first.
		status = "paid"
	case "incomplete":
		// The first payment still waits.
		return nil
	default:
		// canceled, unpaid, incomplete_expired, paused. Only the
		// workspace's own subscription ends it, and only a paid one: a
		// trial stays a trial.
		if !ours || space.Status != "paid" {
			return nil
		}
		status = "ended"
	}
	err = c.store.SetBilling(space.ID, workspace.Billing{Status: status, Customer: sub.Customer, Subscription: sub.ID, PaidUntil: sub.PaidUntil()})
	if err != nil {
		return err
	}
	slog.Info("plan changed", "workspace", space.ID, "status", status, "subscription", sub.ID)
	if space.Status == "paid" && status == "ended" {
		c.mail(ctx, space.Mail, "Your "+build.Name+" cloud subscription ended",
			"Your subscription of "+build.Name+" cloud ended. Your servers still report, but no alert goes out, and no new copy of a backup is taken.\n\n"+
				"Subscribe again: "+c.appURL+"/cloud")
	}
	return nil
}

// cancelSubscription ends the subscription of a workspace that is being
// deleted, and says so by mail.
func (c *Cloud) cancelSubscription(ctx context.Context, space workspace.Workspace) error {
	if space.Status != "paid" || space.StripeSubscription == "" {
		return nil
	}
	if c.Stripe == nil {
		return errors.New("this site has no Stripe account to cancel with")
	}
	if err := c.Stripe.Cancel(ctx, space.StripeSubscription); err != nil {
		return err
	}
	slog.Info("subscription cancelled", "workspace", space.ID, "subscription", space.StripeSubscription)
	c.mail(ctx, space.Mail, "Your "+build.Name+" cloud subscription is cancelled",
		"You deleted your "+build.Name+" cloud account, so its subscription is cancelled. No more charges.")
	return nil
}

// mail sends one mail; one that does not go out is only logged.
func (c *Cloud) mail(ctx context.Context, to, subject, text string) {
	if err := c.mailer.Send(ctx, mailer.Message{To: to, Subject: subject, Text: text}); err != nil {
		slog.Error("mail not sent", "subject", subject, "error", err)
	}
}

// billingURL is where `moin cloud` sends you about money: the portal once
// Stripe knows you, else /cloud to subscribe.
func (c *Cloud) billingURL(space workspace.Workspace) string {
	if space.StripeCustomer != "" {
		return c.appURL + "/cloud/billing"
	}
	return c.appURL + "/cloud"
}
