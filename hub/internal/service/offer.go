package service

import (
	"log/slog"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
)

// OfferService gives an email address the offer that waits for it. Billing
// off has none: a self-hosted hub does not set it up.
type OfferService struct {
	offers *repository.Offers
	admin  *repository.Admin
	now    func() time.Time
}

func NewOfferService(offers *repository.Offers, admin *repository.Admin) *OfferService {
	return &OfferService{offers: offers, admin: admin, now: time.Now}
}

// Claim applies the offer of the user's address to the workspace. Only a
// verified address claims, and only for the first workspace it created.
func (s *OfferService) Claim(user *model.User, workspaceID string) (bool, error) {
	if user.EmailVerifiedAt == nil {
		return false, nil
	}
	first, err := s.admin.FirstWorkspace(user.ID)
	if err != nil || first == nil || first.ID != workspaceID {
		return false, err
	}
	return s.offers.Claim(offerEmail(user.Email), user.ID, workspaceID, s.now().UTC())
}

// ClaimOpen applies every open offer to the first workspace of the account
// with that address, and answers how many it applied. A failure at one
// account does not stop the others: it stays open for the next run.
func (s *OfferService) ClaimOpen() (int, error) {
	users, err := s.offers.UsersWithOpenOffer(s.now().UTC())
	if err != nil {
		return 0, err
	}
	claimed := 0
	for i := range users {
		user := &users[i]
		first, err := s.admin.FirstWorkspace(user.ID)
		if err != nil {
			slog.Error("find the workspace for an offer", "error", err, "user_id", user.ID)
			continue
		}
		if first == nil {
			continue
		}
		applied, err := s.Claim(user, first.ID)
		if err != nil {
			slog.Error("claim an offer", "error", err, "user_id", user.ID)
			continue
		}
		if applied {
			claimed++
		}
	}
	return claimed, nil
}

// Apply gives the workspace of the user the offer of another address, for
// someone who signed up with a different one.
func (s *OfferService) Apply(email, userID, workspaceID string) (bool, error) {
	return s.offers.Claim(offerEmail(email), userID, workspaceID, s.now().UTC())
}

func offerEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
