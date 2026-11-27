package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// sign is the Stripe-Signature header Stripe sends for payload at time at.
func sign(secret string, payload []byte, at time.Time) string {
	stamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "." + string(payload)))
	return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// A webhook counts when Stripe signed it with the secret, a moment ago.
func TestEventSignature(t *testing.T) {
	s := New(Settings{WebhookSecret: "whsec_test"})
	payload := []byte(`{"id":"evt_1","type":"customer.subscription.updated","data":{"object":{"id":"sub_1"}}}`)
	now := time.Now()

	event, err := s.Event(payload, sign("whsec_test", payload, now), now)
	if err != nil || event.Type != "customer.subscription.updated" || string(event.Data.Object) != `{"id":"sub_1"}` {
		t.Fatalf("signed event: %+v, %v", event, err)
	}
	// Stripe sends two v1 while a secret is being rolled: one is enough.
	both := sign("whsec_old", payload, now) + ",v1=" + sign("whsec_test", payload, now)[len("t=1700000000,v1="):]
	if _, err := s.Event(payload, both, now); err != nil {
		t.Errorf("two signatures, one right: %v", err)
	}
	for name, header := range map[string]string{
		"another secret": sign("whsec_other", payload, now),
		"too old":        sign("whsec_test", payload, now.Add(-10*time.Minute)),
		"no header":      "",
		"no v1":          "t=" + strconv.FormatInt(now.Unix(), 10),
	} {
		if _, err := s.Event(payload, header, now); !errors.Is(err, ErrSignature) {
			t.Errorf("%s: %v, want ErrSignature", name, err)
		}
	}
	changed := []byte(`{"id":"evt_1","type":"customer.subscription.deleted"}`)
	if _, err := s.Event(changed, sign("whsec_test", payload, now), now); !errors.Is(err, ErrSignature) {
		t.Errorf("a changed payload: %v", err)
	}
}

// fakeStripe answers like Stripe and keeps the last form it got.
func fakeStripe(t *testing.T) (*Stripe, *url.Values) {
	t.Helper()
	got := &url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key, _, _ := r.BasicAuth(); key != "sk_test" || r.Header.Get("Stripe-Version") != version {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		r.ParseForm()
		*got = r.PostForm
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/checkout/sessions":
			w.Write([]byte(`{"url":"https://checkout.stripe.test/c/1"}`))
		case "DELETE /v1/subscriptions/sub_gone":
			http.Error(w, `{"error":{"message":"No such subscription"}}`, http.StatusNotFound)
		default:
			http.Error(w, `{"error":{"message":"unknown"}}`, http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	return New(Settings{SecretKey: "sk_test", API: server.URL, Product: "prod_cloud", Monthly: "price_m", Yearly: "price_y"}), got
}

// A checkout sells the price of the interval through Managed Payments,
// says it is for businesses, and carries the workspace.
func TestCheckout(t *testing.T) {
	s, got := fakeStripe(t)
	address, err := s.Checkout(context.Background(), Checkout{Workspace: "w1", Mail: "ada@example.com", Interval: "yearly"})
	if err != nil || address != "https://checkout.stripe.test/c/1" {
		t.Fatalf("checkout: %q, %v", address, err)
	}
	for key, want := range map[string]string{
		"mode": "subscription", "line_items[0][price]": "price_y", "managed_payments[enabled]": "true",
		"customer_email": "ada@example.com", "client_reference_id": "w1",
		"subscription_data[metadata][workspace_id]": "w1", "custom_text[submit][message]": BusinessOnly,
		"allow_promotion_codes": "true", "discounts[0][coupon]": "", "customer": "",
	} {
		if got.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, got.Get(key), want)
		}
	}
	if _, err := s.Checkout(context.Background(), Checkout{Workspace: "w1", Interval: "weekly"}); err == nil {
		t.Error("a weekly checkout started")
	}
}

// An early backer's coupon is applied, and a customer Stripe knows keeps
// its account.
func TestCheckoutCouponAndCustomer(t *testing.T) {
	s, got := fakeStripe(t)
	_, err := s.Checkout(context.Background(), Checkout{Workspace: "w1", Mail: "ada@example.com", Customer: "cus_old", Coupon: "EARLY", Interval: "monthly"})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"line_items[0][price]": "price_m", "discounts[0][coupon]": "EARLY", "allow_promotion_codes": "",
		"customer": "cus_old", "customer_email": "",
	} {
		if got.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, got.Get(key), want)
		}
	}
}

// Every price of the product counts, also an old one; another product
// doesn't.
func TestSells(t *testing.T) {
	s := New(Settings{Product: "prod_cloud", Monthly: "price_m", Yearly: "price_y"})
	with := func(price, product string) Subscription {
		var item Item
		item.Price.ID, item.Price.Product = price, product
		var sub Subscription
		sub.Items.Data = []Item{item}
		return sub
	}
	for _, c := range []struct {
		price, product string
		want           bool
	}{
		{"price_y", "prod_cloud", true},
		{"price_2025_monthly", "prod_cloud", true},
		{"price_m", "", true},
		{"price_other", "prod_other", false},
		{"", "", false},
	} {
		if got := s.Sells(with(c.price, c.product)); got != c.want {
			t.Errorf("%s of %s: %v, want %v", c.price, c.product, got, c.want)
		}
	}
}

// Cancelling a subscription Stripe doesn't know is no error: it is gone.
func TestCancelGone(t *testing.T) {
	s, _ := fakeStripe(t)
	if err := s.Cancel(context.Background(), "sub_gone"); err != nil {
		t.Errorf("cancel a gone subscription: %v", err)
	}
	if err := s.Cancel(context.Background(), "sub_other"); err == nil {
		t.Error("an error of Stripe was no error")
	}
}
