// Package stripe is the little of Stripe that moin cloud needs: a
// checkout to subscribe, the customer portal, a subscription to read or
// cancel, and the webhooks Stripe sends. It speaks to the Stripe API with
// net/http: forms go in, JSON comes back.
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// version is the API version every call asks for, so Stripe's answers
// keep the shape this package reads. Managed Payments needs one of
// 2025-03-31 or later.
const version = "2026-04-22.dahlia"

// Settings are the keys and ids from the Stripe dashboard.
type Settings struct {
	// SecretKey calls the API; WebhookSecret checks what Stripe sends.
	SecretKey     string
	WebhookSecret string
	// API is https://api.stripe.com, or a fake one in tests.
	API string
	// Product is what moin cloud sells. Every price of it counts, also an
	// old one an early customer still pays.
	Product string
	// Monthly and Yearly are the two prices a new checkout sells.
	Monthly string
	Yearly  string
}

// Stripe talks to one Stripe account.
type Stripe struct {
	settings Settings
	client   *http.Client
}

// New is the Stripe account of settings.
func New(settings Settings) *Stripe {
	settings.API = strings.TrimRight(settings.API, "/")
	return &Stripe{settings: settings, client: &http.Client{Timeout: 30 * time.Second}}
}

// Checkout is what a new subscription needs.
type Checkout struct {
	// Workspace is the id of the workspace; it comes back in the webhooks.
	Workspace string
	// Mail fills the form. A Customer that Stripe knows already is used
	// instead, so an old customer keeps one account in Stripe.
	Mail     string
	Customer string
	// Interval is monthly or yearly.
	Interval string
	// Coupon is the early backers' discount, when the workspace has one.
	// Without it, the buyer may type a promotion code. Stripe refuses both.
	Coupon string
	// SuccessURL and CancelURL are where the buyer goes back to.
	SuccessURL string
	CancelURL  string
}

// Price is the price of interval, or "" for anything but monthly and
// yearly.
func (s *Stripe) Price(interval string) string {
	switch interval {
	case "monthly":
		return s.settings.Monthly
	case "yearly":
		return s.settings.Yearly
	}
	return ""
}

// Checkout starts a checkout and returns its address. Stripe Managed
// Payments sells it: Stripe is the seller, and handles tax. Managed
// Payments refuses custom_text, so /cloud says it is for businesses.
func (s *Stripe) Checkout(ctx context.Context, c Checkout) (string, error) {
	price := s.Price(c.Interval)
	if price == "" {
		return "", fmt.Errorf("pay monthly or yearly, not %q", c.Interval)
	}
	form := url.Values{
		"mode":                      {"subscription"},
		"line_items[0][price]":      {price},
		"line_items[0][quantity]":   {"1"},
		"success_url":               {c.SuccessURL},
		"cancel_url":                {c.CancelURL},
		"client_reference_id":       {c.Workspace},
		"metadata[workspace_id]":    {c.Workspace},
		"managed_payments[enabled]": {"true"},
		"subscription_data[metadata][workspace_id]": {c.Workspace},
	}
	if c.Customer != "" {
		form.Set("customer", c.Customer)
	} else {
		form.Set("customer_email", c.Mail)
	}
	if c.Coupon != "" {
		form.Set("discounts[0][coupon]", c.Coupon)
	} else {
		form.Set("allow_promotion_codes", "true")
	}
	var session struct {
		URL string `json:"url"`
	}
	if _, err := s.call(ctx, http.MethodPost, "/v1/checkout/sessions", form, &session); err != nil {
		return "", err
	}
	if session.URL == "" {
		return "", errors.New("stripe: the checkout has no address")
	}
	return session.URL, nil
}

// Portal is the address of the customer portal: invoices, the card, and
// cancel. Back leads to moin cloud.
func (s *Stripe) Portal(ctx context.Context, customer, back string) (string, error) {
	var session struct {
		URL string `json:"url"`
	}
	form := url.Values{"customer": {customer}, "return_url": {back}}
	if _, err := s.call(ctx, http.MethodPost, "/v1/billing_portal/sessions", form, &session); err != nil {
		return "", err
	}
	return session.URL, nil
}

// Subscription is what moin cloud reads of a subscription.
type Subscription struct {
	ID       string            `json:"id"`
	Customer string            `json:"customer"`
	Status   string            `json:"status"`
	Metadata map[string]string `json:"metadata"`
	Items    struct {
		Data []Item `json:"data"`
	} `json:"items"`
}

// Item is one line of a subscription: a price, paid until the end of the
// period.
type Item struct {
	CurrentPeriodEnd int64 `json:"current_period_end"`
	Price            struct {
		ID      string `json:"id"`
		Product string `json:"product"`
	} `json:"price"`
}

// PaidUntil is the end of the period paid for.
func (sub Subscription) PaidUntil() time.Time {
	var end int64
	for _, item := range sub.Items.Data {
		end = max(end, item.CurrentPeriodEnd)
	}
	if end == 0 {
		return time.Time{}
	}
	return time.Unix(end, 0).UTC()
}

// Sells says whether sub buys moin cloud: one of its prices is of the
// product, or is one of the two prices. Any other product of the same
// Stripe account is none of moin cloud's business.
func (s *Stripe) Sells(sub Subscription) bool {
	for _, item := range sub.Items.Data {
		price := item.Price
		if (price.Product != "" && price.Product == s.settings.Product) ||
			(price.ID != "" && (price.ID == s.settings.Monthly || price.ID == s.settings.Yearly)) {
			return true
		}
	}
	return false
}

// Subscription reads a subscription as Stripe knows it now. Webhooks can
// come late and out of order; the subscription itself is always new.
func (s *Stripe) Subscription(ctx context.Context, id string) (Subscription, error) {
	var sub Subscription
	_, err := s.call(ctx, http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), nil, &sub)
	return sub, err
}

// Cancel ends a subscription now. One Stripe doesn't know is ended
// already.
func (s *Stripe) Cancel(ctx context.Context, id string) error {
	status, err := s.call(ctx, http.MethodDelete, "/v1/subscriptions/"+url.PathEscape(id), nil, nil)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

// call sends form to path and reads the JSON answer into out. It returns
// the status code; an answer that is not 2xx is an error with Stripe's
// message.
func (s *Stripe) call(ctx context.Context, method, path string, form url.Values, out any) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, s.settings.API+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	request.SetBasicAuth(s.settings.SecretKey, "")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Stripe-Version", version)
	response, err := s.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("stripe: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response.StatusCode, fmt.Errorf("stripe: %w", err)
	}
	if response.StatusCode/100 != 2 {
		var answer struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &answer)
		return response.StatusCode, fmt.Errorf("stripe: %s %s: HTTP %d: %s", method, path, response.StatusCode, answer.Error.Message)
	}
	if out == nil {
		return response.StatusCode, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return response.StatusCode, fmt.Errorf("stripe: %s %s: %w", method, path, err)
	}
	return response.StatusCode, nil
}

// Event is one webhook: what happened, and the object it happened to.
type Event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// ErrSignature is a webhook Stripe did not sign, or signed too long ago.
var ErrSignature = errors.New("stripe: the webhook's signature is wrong or too old")

// tolerance is how old a signed webhook may be, against replays.
const tolerance = 5 * time.Minute

// Event checks that Stripe signed payload, by the Stripe-Signature header
// signature, and reads it. The header is like t=1700000000,v1=5257a8…:
// the time, and an HMAC-SHA256 of "time.payload" with the webhook secret.
func (s *Stripe) Event(payload []byte, signature string, now time.Time) (Event, error) {
	var event Event
	var stamp string
	var signatures []string
	for _, part := range strings.Split(signature, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch key {
		case "t":
			stamp = value
		case "v1":
			signatures = append(signatures, value)
		}
	}
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || s.settings.WebhookSecret == "" {
		return event, ErrSignature
	}
	if age := now.Sub(time.Unix(seconds, 0)); age > tolerance || age < -tolerance {
		return event, ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(s.settings.WebhookSecret))
	mac.Write([]byte(stamp + "."))
	mac.Write(payload)
	want := mac.Sum(nil)
	for _, text := range signatures {
		got, err := hex.DecodeString(text)
		if err == nil && hmac.Equal(got, want) {
			return event, json.Unmarshal(payload, &event)
		}
	}
	return event, ErrSignature
}
