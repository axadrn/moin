package routes

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/counts"
	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/cloud/internal/github"
	"github.com/axadrn/moin/cloud/internal/handler"
	"github.com/axadrn/moin/cloud/internal/stripe"
	"github.com/axadrn/moin/cloud/internal/watchers"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

const webhookSecret = "whsec_test"

// fakeStripe answers like Stripe: checkouts, the portal, and the
// subscriptions a test puts in subs. It keeps what it was asked.
type fakeStripe struct {
	mu        sync.Mutex
	subs      map[string]map[string]any
	checkout  url.Values
	cancelled []string
}

func (f *fakeStripe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ParseForm()
	id := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/checkout/sessions":
		f.checkout = r.PostForm
		w.Write([]byte(`{"url":"https://checkout.stripe.test/c/1"}`))
	case r.Method == "POST" && r.URL.Path == "/v1/billing_portal/sessions":
		w.Write([]byte(`{"url":"https://billing.stripe.test/p/` + r.PostForm.Get("customer") + `"}`))
	case r.Method == "GET" && f.subs[id] != nil:
		json.NewEncoder(w).Encode(f.subs[id])
	case r.Method == "DELETE" && f.subs[id] != nil:
		f.cancelled = append(f.cancelled, id)
		f.subs[id]["status"] = "canceled"
		json.NewEncoder(w).Encode(f.subs[id])
	default:
		http.Error(w, `{"error":{"message":"No such thing"}}`, http.StatusNotFound)
	}
}

// subscription puts a subscription into the fake Stripe.
func (f *fakeStripe) subscription(id, customer, status, price, product, workspaceID string, end time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[id] = map[string]any{
		"id": id, "customer": customer, "status": status,
		"metadata": map[string]string{"workspace_id": workspaceID},
		"items": map[string]any{"data": []any{map[string]any{
			"current_period_end": end.Unix(),
			"price":              map[string]string{"id": price, "product": product},
		}}},
	}
}

// billingSite is the site with moin cloud and a fake Stripe, and its
// database for what the import would have put in.
func billingSite(t *testing.T) (*httptest.Server, *http.Client, *outbox, *fakeStripe, *workspace.Store, *sql.DB) {
	t.Helper()
	fake := &fakeStripe{subs: map[string]map[string]any{}}
	stripeServer := httptest.NewServer(fake)
	t.Cleanup(stripeServer.Close)

	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store := workspace.NewStore(database)
	mails := &outbox{}
	server := httptest.NewUnstartedServer(nil)
	appURL := "http://" + server.Listener.Addr().String()
	cloud := handler.NewCloud(store, watchers.New(store, counts.New(database), mails, appURL, [32]byte{}), mails, github.New("", "", "", ""), appURL, false)
	cloud.Stripe = stripe.New(stripe.Settings{
		SecretKey: "sk_test", WebhookSecret: webhookSecret, API: stripeServer.URL,
		Product: "prod_cloud", Monthly: "price_monthly", Yearly: "price_yearly",
	})
	site, err := New(&config.Config{RenderConfig: config.RenderConfig{AppURL: appURL}}, cloud)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = site
	server.Start()
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	// The browser stops at Stripe's pages, which a test can't open.
	browser := &http.Client{Jar: jar, CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		if !strings.HasPrefix(r.URL.String(), appURL) {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return server, browser, mails, fake, store, database
}

// sendEvent sends a webhook as Stripe would, signed, and returns the
// status code.
func sendEvent(t *testing.T, server *httptest.Server, secret, kind string, object map[string]any) int {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"id": "evt_" + kind, "type": kind, "data": map[string]any{"object": object}})
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "." + string(payload)))
	request, _ := http.NewRequest("POST", server.URL+"/webhooks/payment", bytes.NewReader(payload))
	request.Header.Set("Stripe-Signature", "t="+stamp+",v1="+hex.EncodeToString(mac.Sum(nil)))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}

// From the Subscribe button to paid, cancelled at the period's end, and
// ended, with one mail.
func TestSubscribe(t *testing.T) {
	server, browser, mails, fake, store, _ := billingSite(t)
	signIn(t, server, browser, mails, "ada@example.com")
	space, _ := store.SignIn("ada@example.com")
	if page := body(t, browser, server.URL+"/cloud"); !strings.Contains(page, "$49 a year") || strings.Contains(page, `href="/cloud/billing"`) {
		t.Fatalf("a trial shows the offer, without billing: %s", page)
	}

	response, err := browser.PostForm(server.URL+"/cloud/subscribe", url.Values{"interval": {"yearly"}})
	if err != nil || response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "https://checkout.stripe.test/c/1" {
		t.Fatalf("subscribe: %v %v to %q", err, response.Status, response.Header.Get("Location"))
	}
	if fake.checkout.Get("line_items[0][price]") != "price_yearly" || fake.checkout.Get("client_reference_id") != space.ID || fake.checkout.Get("customer_email") != "ada@example.com" {
		t.Errorf("checkout: %v", fake.checkout)
	}

	// Stripe made the subscription; the checkout's event names it.
	end := time.Now().Add(365 * 24 * time.Hour).Truncate(time.Second).UTC()
	fake.subscription("sub_1", "cus_1", "active", "price_yearly", "prod_cloud", space.ID, end)
	if status := sendEvent(t, server, webhookSecret, "checkout.session.completed", map[string]any{"id": "cs_1", "customer": "cus_1", "subscription": "sub_1", "client_reference_id": space.ID}); status != http.StatusNoContent {
		t.Fatalf("checkout event: %d", status)
	}
	space, _ = store.Workspace(space.ID)
	if space.Status != "paid" || space.StripeCustomer != "cus_1" || space.StripeSubscription != "sub_1" || !space.PaidUntil.Equal(end) {
		t.Fatalf("after the checkout: %+v", space)
	}
	page := body(t, browser, server.URL+"/cloud")
	if strings.Contains(page, "$49 a year") || !strings.Contains(page, `href="/cloud/billing"`) || !strings.Contains(page, "Paid until") {
		t.Errorf("a paid workspace shows billing, no offer: %s", page)
	}
	response, _ = browser.Get(server.URL + "/cloud/billing")
	if response.Header.Get("Location") != "https://billing.stripe.test/p/cus_1" {
		t.Errorf("billing went to %q", response.Header.Get("Location"))
	}

	// Cancelled in the portal: paid until the period ends, no mail yet.
	sent := len(mails.sent)
	if status := sendEvent(t, server, webhookSecret, "customer.subscription.updated", map[string]any{"id": "sub_1"}); status != http.StatusNoContent {
		t.Fatalf("updated event: %d", status)
	}
	if space, _ = store.Workspace(space.ID); space.Status != "paid" || len(mails.sent) != sent {
		t.Errorf("after the update: %+v, %d mails", space, len(mails.sent)-sent)
	}

	// The period is over.
	fake.subscription("sub_1", "cus_1", "canceled", "price_yearly", "prod_cloud", space.ID, end)
	for range 2 {
		if status := sendEvent(t, server, webhookSecret, "customer.subscription.deleted", map[string]any{"id": "sub_1"}); status != http.StatusNoContent {
			t.Fatalf("deleted event: %d", status)
		}
	}
	if space, _ = store.Workspace(space.ID); space.Status != "ended" {
		t.Errorf("after the end: %+v", space)
	}
	if len(mails.sent) != sent+1 || !strings.Contains(mails.last(t).Subject, "ended") {
		t.Errorf("one mail about the end, got %d: %+v", len(mails.sent)-sent, mails.last(t))
	}
	if page := body(t, browser, server.URL+"/cloud"); !strings.Contains(page, "Alerts are off.") || strings.Contains(page, "trial") {
		t.Errorf("an ended subscription is no trial: %s", page)
	}
}

// An event Stripe didn't sign changes nothing.
func TestWebhookSignature(t *testing.T) {
	server, _, _, fake, store, _ := billingSite(t)
	space, _ := store.SignIn("ada@example.com")
	fake.subscription("sub_1", "cus_1", "active", "price_yearly", "prod_cloud", space.ID, time.Now().Add(time.Hour))
	if status := sendEvent(t, server, "whsec_other", "customer.subscription.updated", map[string]any{"id": "sub_1"}); status != http.StatusBadRequest {
		t.Errorf("a wrong signature: %d", status)
	}
	if space, _ = store.Workspace(space.ID); space.Status != "trial" {
		t.Errorf("a wrong signature changed the plan: %+v", space)
	}
}

// A customer of the old service is found by their customer in Stripe, on a
// price moin no longer sells. Another product of the account counts not.
func TestWebhookOldCustomer(t *testing.T) {
	server, _, _, fake, store, database := billingSite(t)
	_, err := database.Exec(`INSERT INTO workspaces (id, mail, status, stripe_customer, stripe_subscription, created)
		VALUES ('old-1', 'grace@example.com', 'ended', 'cus_old', 'sub_old', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	// The old service's subscription carries no workspace id this site knows.
	fake.subscription("sub_old", "cus_old", "active", "price_2025_monthly", "prod_cloud", "", end)
	fake.subscription("sub_shop", "cus_old", "active", "price_shirt", "prod_shirt", "", end)
	if status := sendEvent(t, server, webhookSecret, "customer.subscription.updated", map[string]any{"id": "sub_shop"}); status != http.StatusNoContent {
		t.Fatalf("event: %d", status)
	}
	if space, _ := store.Workspace("old-1"); space.Status != "ended" {
		t.Errorf("another product changed the plan: %+v", space)
	}
	if status := sendEvent(t, server, webhookSecret, "customer.subscription.updated", map[string]any{"id": "sub_old"}); status != http.StatusNoContent {
		t.Fatalf("event: %d", status)
	}
	if space, _ := store.Workspace("old-1"); space.Status != "paid" || !space.PaidUntil.Equal(end) {
		t.Errorf("an old price of the product: %+v", space)
	}
}

// An early backer's coupon goes into the checkout, and an old customer
// keeps their customer in Stripe.
func TestSubscribeCoupon(t *testing.T) {
	server, browser, mails, fake, _, database := billingSite(t)
	_, err := database.Exec(`INSERT INTO workspaces (id, mail, status, stripe_customer, coupon, created)
		VALUES ('old-2', 'grace@example.com', 'trial', 'cus_old', 'EARLY50', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	signIn(t, server, browser, mails, "grace@example.com")
	response, err := browser.PostForm(server.URL+"/cloud/subscribe", url.Values{"interval": {"monthly"}})
	if err != nil || response.StatusCode != http.StatusSeeOther {
		t.Fatalf("subscribe: %v %v", err, response.Status)
	}
	for key, want := range map[string]string{"discounts[0][coupon]": "EARLY50", "customer": "cus_old", "line_items[0][price]": "price_monthly", "allow_promotion_codes": ""} {
		if got := fake.checkout.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if response, _ := browser.PostForm(server.URL+"/cloud/subscribe", url.Values{"interval": {"weekly"}}); response.StatusCode != http.StatusBadRequest {
		t.Errorf("weekly: %v", response.Status)
	}
}

// Deleting a paid account cancels its subscription first, with one mail.
func TestDeleteAccountCancels(t *testing.T) {
	server, browser, mails, fake, store, _ := billingSite(t)
	signIn(t, server, browser, mails, "ada@example.com")
	space, _ := store.SignIn("ada@example.com")
	fake.subscription("sub_1", "cus_1", "active", "price_monthly", "prod_cloud", space.ID, time.Now().Add(time.Hour))
	if status := sendEvent(t, server, webhookSecret, "customer.subscription.updated", map[string]any{"id": "sub_1"}); status != http.StatusNoContent {
		t.Fatalf("event: %d", status)
	}
	response, err := browser.PostForm(server.URL+"/cloud/delete", url.Values{"mail": {"ada@example.com"}})
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("delete: %v %v", err, response.Status)
	}
	if len(fake.cancelled) != 1 || fake.cancelled[0] != "sub_1" {
		t.Errorf("cancelled %v", fake.cancelled)
	}
	if _, err := store.Workspace(space.ID); err == nil {
		t.Error("the workspace is still there")
	}
	if !strings.Contains(mails.last(t).Subject, "cancelled") {
		t.Errorf("last mail: %+v", mails.last(t))
	}
	// Stripe tells of the end afterwards; there is nobody to tell.
	if status := sendEvent(t, server, webhookSecret, "customer.subscription.deleted", map[string]any{"id": "sub_1"}); status != http.StatusNoContent {
		t.Errorf("the end of a deleted account's subscription: %d", status)
	}
}
