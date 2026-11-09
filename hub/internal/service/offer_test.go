package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/jmoiron/sqlx"
)

const testCoupon = "coupon_test"

func addOffer(t *testing.T, database *sqlx.DB, email string, expiresAt *time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO offers (email, trial_days, coupon_id, reason, expires_at, created_at) VALUES ($1, 30, $2, 'test', $3, $4)`, email, testCoupon, expiresAt, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func offerClaimed(t *testing.T, database *sqlx.DB, email string) bool {
	t.Helper()
	var claimed int
	if err := database.Get(&claimed, `SELECT COUNT(*) FROM offers WHERE email = $1 AND claimed_at IS NOT NULL`, email); err != nil {
		t.Fatal(err)
	}
	return claimed == 1
}

func newOffers(database *sqlx.DB) *OfferService {
	return NewOfferService(repository.NewOffers(database), repository.NewAdmin(database))
}

func subscriptionOf(t *testing.T, database *sqlx.DB, workspaceID string) *model.Subscription {
	t.Helper()
	subscription, err := repository.NewSubscriptions(database).ByWorkspaceID(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return subscription
}

func assertOffered(t *testing.T, subscription *model.Subscription) {
	t.Helper()
	if subscription.PlanID != model.SubscriptionPlanPro || subscription.Status != model.SubscriptionStatusTrialing {
		t.Fatalf("plan %s status %s", subscription.PlanID, subscription.Status)
	}
	if subscription.CouponID == nil || *subscription.CouponID != testCoupon {
		t.Fatalf("coupon %v", subscription.CouponID)
	}
	want := time.Now().AddDate(0, 0, 30)
	if subscription.CurrentPeriodEnd == nil || subscription.CurrentPeriodEnd.Sub(want).Abs() > time.Minute {
		t.Fatalf("period end %v, want about %v", subscription.CurrentPeriodEnd, want)
	}
}

func TestOfferIsClaimedOnce(t *testing.T) {
	database := testutil.NewDatabase(t)
	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	addOffer(t, database, "ada@example.com", nil)
	offers := newOffers(database)

	if applied, err := offers.Claim(ada.User, ada.Workspace.ID); err != nil || !applied {
		t.Fatalf("first claim = %v, %v", applied, err)
	}
	assertOffered(t, subscriptionOf(t, database, ada.Workspace.ID))
	before := subscriptionOf(t, database, ada.Workspace.ID).UpdatedAt
	if applied, err := offers.Claim(ada.User, ada.Workspace.ID); err != nil || applied {
		t.Fatalf("second claim = %v, %v", applied, err)
	}
	if after := subscriptionOf(t, database, ada.Workspace.ID).UpdatedAt; !after.Equal(before) {
		t.Fatalf("second claim changed the subscription: %v, %v", before, after)
	}
}

func TestOfferMatchesTheAddressHoweverItIsWritten(t *testing.T) {
	database := testutil.NewDatabase(t)
	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: " Ada@Example.COM", Verified: true})
	addOffer(t, database, "ada@example.com", nil)
	if applied, err := newOffers(database).Claim(ada.User, ada.Workspace.ID); err != nil || !applied {
		t.Fatalf("claim = %v, %v", applied, err)
	}
}

func TestOfferIsNotClaimed(t *testing.T) {
	past := time.Now().UTC().Add(-time.Hour)
	tests := []struct {
		name    string
		options testutil.UserOptions
		expires *time.Time
		prepare func(t *testing.T, database *sqlx.DB, fixture testutil.UserFixture)
	}{
		{name: "expired", options: testutil.UserOptions{Verified: true}, expires: &past},
		{name: "unverified address", options: testutil.UserOptions{}},
		{name: "paying workspace", options: testutil.UserOptions{Verified: true}, prepare: func(t *testing.T, database *sqlx.DB, fixture testutil.UserFixture) {
			provider := "sub_test"
			fixture.Subscription.PlanID, fixture.Subscription.ProviderSubscriptionID = model.SubscriptionPlanPro, &provider
			if err := repository.NewSubscriptions(database).Update(fixture.Subscription); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "free forever", options: testutil.UserOptions{Verified: true}, prepare: func(t *testing.T, database *sqlx.DB, fixture testutil.UserFixture) {
			fixture.Subscription.MakeForeverFree(time.Now().UTC())
			if err := repository.NewSubscriptions(database).Update(fixture.Subscription); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := testutil.NewDatabase(t)
			test.options.ID, test.options.Email = "ada", "ada@example.com"
			fixture := testutil.SeedUser(t, database, test.options)
			addOffer(t, database, "ada@example.com", test.expires)
			if test.prepare != nil {
				test.prepare(t, database, fixture)
			}
			before := *subscriptionOf(t, database, fixture.Workspace.ID)
			if applied, err := newOffers(database).Claim(fixture.User, fixture.Workspace.ID); err != nil || applied {
				t.Fatalf("claim = %v, %v", applied, err)
			}
			if offerClaimed(t, database, "ada@example.com") {
				t.Fatal("the offer was marked claimed")
			}
			after := subscriptionOf(t, database, fixture.Workspace.ID)
			if after.Status != before.Status || after.PlanID != before.PlanID || after.CouponID != nil {
				t.Fatalf("subscription changed: %+v", after)
			}
		})
	}
}

func TestANewWorkspaceClaimsTheOfferOfItsCreator(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	environment.workspaceService.UseOffers(newOffers(environment.database))
	addOffer(t, environment.database, "ada@example.com", nil)

	// GitHub has verified the address.
	user, err := environment.auth.AuthenticateOAuth("Ada@Example.com", "github")
	if err != nil {
		t.Fatal(err)
	}
	first, err := environment.workspaceService.Create(user.ID, "Ada's")
	if err != nil {
		t.Fatal(err)
	}
	assertOffered(t, subscriptionOf(t, environment.database, first.ID))

	second, err := environment.workspaceService.Create(user.ID, "Ada's second")
	if err != nil {
		t.Fatal(err)
	}
	if coupon := subscriptionOf(t, environment.database, second.ID).CouponID; coupon != nil {
		t.Fatalf("second workspace has coupon %q", *coupon)
	}
}

func TestOfferSkipsASecondWorkspaceAndSomeoneElses(t *testing.T) {
	database := testutil.NewDatabase(t)
	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	grace := testutil.SeedUser(t, database, testutil.UserOptions{ID: "grace", Email: "grace@example.com", Verified: true})
	addOffer(t, database, "grace@example.com", nil)
	offers := newOffers(database)

	// Grace is invited to Ada's workspace and has none of her own to count.
	if _, err := database.Exec(`DELETE FROM workspaces WHERE id = $1`, grace.Workspace.ID); err != nil {
		t.Fatal(err)
	}
	if applied, err := offers.Claim(grace.User, ada.Workspace.ID); err != nil || applied {
		t.Fatalf("claim through someone else's workspace = %v, %v", applied, err)
	}
	if offerClaimed(t, database, "grace@example.com") {
		t.Fatal("the offer was marked claimed")
	}
	if coupon := subscriptionOf(t, database, ada.Workspace.ID).CouponID; coupon != nil {
		t.Fatalf("Ada's workspace got coupon %q", *coupon)
	}
}

func TestOfferStaysWithTheWorkspace(t *testing.T) {
	database := testutil.NewDatabase(t)
	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	addOffer(t, database, "ada@example.com", nil)
	if applied, err := newOffers(database).Claim(ada.User, ada.Workspace.ID); err != nil || !applied {
		t.Fatalf("claim = %v, %v", applied, err)
	}
	if _, err := database.Exec(`UPDATE users SET email = 'new@example.com' WHERE id = 'ada'`); err != nil {
		t.Fatal(err)
	}
	subscriptions := NewSubscriptionService(repository.NewSubscriptions(database), repository.NewAdmin(database))

	// What the webhooks do: start paying, then stop. The coupon survives, so
	// the next checkout has it, and the free days do not come back.
	subscription := subscriptionOf(t, database, ada.Workspace.ID)
	provider := "sub_test"
	subscription.ProviderSubscriptionID, subscription.Status = &provider, model.SubscriptionStatusActive
	if err := subscriptions.UpdateSubscription(subscription); err != nil {
		t.Fatal(err)
	}
	if err := subscriptions.EndPaidAccess(subscriptionOf(t, database, ada.Workspace.ID)); err != nil {
		t.Fatal(err)
	}
	after := subscriptionOf(t, database, ada.Workspace.ID)
	if after.CouponID == nil || *after.CouponID != testCoupon {
		t.Fatalf("coupon after cancel: %v", after.CouponID)
	}
	if after.Status == model.SubscriptionStatusTrialing {
		t.Fatal("the free days came back")
	}
	if applied, err := newOffers(database).Claim(ada.User, ada.Workspace.ID); err != nil || applied {
		t.Fatalf("second claim = %v, %v", applied, err)
	}
}

func TestClaimOpenUsesTheOldestWorkspaceAndSkipsPayers(t *testing.T) {
	database := testutil.NewDatabase(t)
	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	payer := testutil.SeedUser(t, database, testutil.UserOptions{ID: "payer", Email: "payer@example.com", Verified: true})
	addOffer(t, database, "ada@example.com", nil)
	addOffer(t, database, "payer@example.com", nil)
	addOffer(t, database, "nobody@example.com", nil)
	provider := "sub_test"
	payer.Subscription.ProviderSubscriptionID = &provider
	if err := repository.NewSubscriptions(database).Update(payer.Subscription); err != nil {
		t.Fatal(err)
	}

	claimed, err := newOffers(database).ClaimOpen()
	if err != nil || claimed != 1 {
		t.Fatalf("ClaimOpen = %d, %v", claimed, err)
	}
	assertOffered(t, subscriptionOf(t, database, ada.Workspace.ID))
	for _, email := range []string{"payer@example.com", "nobody@example.com"} {
		if offerClaimed(t, database, email) {
			t.Fatalf("%s was marked claimed", email)
		}
	}
}

func TestAdminAppliesAnOfferOfAnotherAddress(t *testing.T) {
	database := testutil.NewDatabase(t)
	operator := testutil.SeedUser(t, database, testutil.UserOptions{ID: "operator", Email: "operator@example.com", Verified: true})
	target := testutil.SeedUser(t, database, testutil.UserOptions{ID: "target", Email: "other@example.com", Verified: true})
	addOffer(t, database, "bought@example.com", nil)
	auth := NewAuthService(nil, nil, repository.NewSessions(database), nil, nil, false, time.Hour, time.Hour, time.Hour, time.Hour, time.Hour)
	auth.UseAPITokens(&APITokenService{tokens: repository.NewAPITokens(database), now: time.Now})
	admin := NewAdminService(repository.NewAdmin(database), repository.NewUsers(database), auth, []string{"operator@example.com"})
	admin.UseOffers(newOffers(database))
	session, err := auth.StartSession(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/auth", nil), operator.User.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := admin.ApplyOffer(target.User, session, target.User.ID, "bought@example.com"); !errors.Is(err, ErrAdminPermission) {
		t.Fatalf("not an operator: %v", err)
	}
	if err := admin.ApplyOffer(operator.User, session, target.User.ID, " Bought@Example.com "); err != nil {
		t.Fatal(err)
	}
	assertOffered(t, subscriptionOf(t, database, target.Workspace.ID))
	if err := admin.ApplyOffer(operator.User, session, target.User.ID, "bought@example.com"); !errors.Is(err, ErrNoOffer) {
		t.Fatalf("applied twice: %v", err)
	}
	if _, err := admin.ClaimOffers(operator.User, session, "wrong"); !errors.Is(err, ErrConfirmMismatch) {
		t.Fatalf("without the word: %v", err)
	}
}

func TestWithoutOffersAWorkspaceIsAsBefore(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	addOffer(t, environment.database, "ada@example.com", nil)
	user, err := environment.auth.AuthenticateOAuth("ada@example.com", "github")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := environment.workspaceService.Create(user.ID, "Ada's")
	if err != nil {
		t.Fatal(err)
	}
	if coupon := subscriptionOf(t, environment.database, workspace.ID).CouponID; coupon != nil {
		t.Fatalf("coupon %q", *coupon)
	}
	if offerClaimed(t, environment.database, "ada@example.com") {
		t.Fatal("the offer was marked claimed")
	}
}
