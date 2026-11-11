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

// adminWithOffers is an admin service with offers, its operator and their
// session.
func adminWithOffers(t *testing.T, database *sqlx.DB) (*AdminService, *model.User, *model.Session) {
	t.Helper()
	operator := testutil.SeedUser(t, database, testutil.UserOptions{ID: "operator", Email: "operator@example.com", Verified: true})
	auth := NewAuthService(nil, nil, repository.NewSessions(database), nil, nil, false, time.Hour, time.Hour, time.Hour, time.Hour, time.Hour)
	auth.UseAPITokens(&APITokenService{tokens: repository.NewAPITokens(database), now: time.Now})
	admin := NewAdminService(repository.NewAdmin(database), repository.NewUsers(database), auth, []string{"operator@example.com"})
	admin.UseOffers(newOffers(database))
	session, err := auth.StartSession(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/auth", nil), operator.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	return admin, operator.User, session
}

func TestAdminAddsAndRemovesAnOffer(t *testing.T) {
	database := testutil.NewDatabase(t)
	admin, operator, session := adminWithOffers(t, database)
	stranger := testutil.SeedUser(t, database, testutil.UserOptions{ID: "stranger", Email: "stranger@example.com", Verified: true})
	input := OfferInput{Email: " New@Example.com ", TrialDays: 365, CouponID: "half", Reason: "goilerplate buyer"}

	if err := admin.CreateOffer(stranger.User, session, input); !errors.Is(err, ErrAdminPermission) {
		t.Fatalf("not an operator: %v", err)
	}
	if err := admin.CreateOffer(operator, session, input); err != nil {
		t.Fatal(err)
	}
	if err := admin.CreateOffer(operator, session, input); !errors.Is(err, repository.ErrOfferExists) {
		t.Fatalf("added twice: %v", err)
	}
	page, err := admin.Offers(operator, session, "new@", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Open != 1 || page.Offers[0].Email != "new@example.com" || page.Offers[0].TrialDays != 365 {
		t.Fatalf("offers %+v", page)
	}
	if len(page.Coupons) != 1 || page.Coupons[0] != "half" {
		t.Fatalf("coupons %v", page.Coupons)
	}
	if err := admin.DeleteOffer(operator, session, "NEW@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := admin.DeleteOffer(operator, session, "new@example.com"); !errors.Is(err, repository.ErrOfferNotOpen) {
		t.Fatalf("removed twice: %v", err)
	}
}

func TestAdminKeepsAClaimedOffer(t *testing.T) {
	database := testutil.NewDatabase(t)
	admin, operator, session := adminWithOffers(t, database)
	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	addOffer(t, database, "ada@example.com", nil)
	if applied, err := newOffers(database).Claim(ada.User, ada.Workspace.ID); err != nil || !applied {
		t.Fatalf("claim = %v, %v", applied, err)
	}
	if err := admin.DeleteOffer(operator, session, "ada@example.com"); !errors.Is(err, repository.ErrOfferNotOpen) {
		t.Fatalf("removed a claimed offer: %v", err)
	}
}

func TestAdminRefusesAnOfferThatMakesNoSense(t *testing.T) {
	database := testutil.NewDatabase(t)
	admin, operator, session := adminWithOffers(t, database)
	for name, input := range map[string]OfferInput{
		"no address":     {Email: "not an address", TrialDays: 30},
		"a name with it": {Email: "Ada <ada@example.com>", TrialDays: 30},
		"no days":        {Email: "ada@example.com", TrialDays: 0},
		"too many days":  {Email: "ada@example.com", TrialDays: model.OfferMaxDays + 1},
		"coupon spaces":  {Email: "ada@example.com", TrialDays: 30, CouponID: "two words"},
	} {
		if err := admin.CreateOffer(operator, session, input); !errors.Is(err, ErrAdminInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAdminWritesANote(t *testing.T) {
	database := testutil.NewDatabase(t)
	admin, operator, session := adminWithOffers(t, database)
	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})

	if err := admin.SetUserNote(ada.User, session, ada.User.ID, "hi"); !errors.Is(err, ErrAdminPermission) {
		t.Fatalf("not an operator: %v", err)
	}
	if err := admin.SetUserNote(operator, session, ada.User.ID, "  Gave the offer of her old address.\r\n"); err != nil {
		t.Fatal(err)
	}
	page, err := admin.User(operator, session, ada.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if page.Note == nil || page.Note.Note != "Gave the offer of her old address." || page.Note.UpdatedByEmail != "operator@example.com" {
		t.Fatalf("note %+v", page.Note)
	}
	if err := admin.SetUserNote(operator, session, ada.User.ID, string(make([]rune, model.UserNoteMax+1))); !errors.Is(err, ErrAdminInput) {
		t.Fatalf("too long: %v", err)
	}
	if err := admin.SetUserNote(operator, session, ada.User.ID, " "); err != nil {
		t.Fatal(err)
	}
	if page, err = admin.User(operator, session, ada.User.ID); err != nil || page.Note != nil {
		t.Fatalf("emptied note %+v, %v", page.Note, err)
	}
}
