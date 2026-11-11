package service

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/axadrn/moin/hub/internal/model"
)

// ErrAdminInput means what an admin typed is not one to keep: the error
// says which field and why.
var ErrAdminInput = errors.New("check what you typed")

// OfferInput is what an admin types to add an offer.
type OfferInput struct {
	Email     string
	TrialDays int
	CouponID  string
	Reason    string
}

// Offers is a page of offers for admins, newest first.
func (s *AdminService) Offers(actor *model.User, actorSession *model.Session, query string, page int) (*model.AdminOfferPage, error) {
	if !s.IsAdmin(actor, actorSession) {
		return nil, ErrAdminPermission
	}
	if s.offers == nil {
		return &model.AdminOfferPage{Page: 1, TotalPages: 1}, nil
	}
	if page < 1 {
		page = 1
	}
	offers, total, err := s.offers.offers.List(query, adminPageSize, (page-1)*adminPageSize)
	if err != nil {
		return nil, err
	}
	open, err := s.offers.offers.CountOpen(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	coupons, err := s.offers.offers.Coupons()
	if err != nil {
		return nil, err
	}
	return &model.AdminOfferPage{
		Offers:     offers,
		Query:      strings.TrimSpace(query),
		Page:       page,
		TotalPages: max(1, (total+adminPageSize-1)/adminPageSize),
		Total:      total,
		Open:       open,
		Coupons:    coupons,
	}, nil
}

// CreateOffer adds an offer that waits for an address. Whoever signs up
// with it gets it; someone already signed up gets it with Apply open
// offers.
func (s *AdminService) CreateOffer(actor *model.User, actorSession *model.Session, input OfferInput) error {
	if !s.IsAdmin(actor, actorSession) {
		return ErrAdminPermission
	}
	if s.offers == nil {
		return ErrNoWorkspace
	}
	offer, err := checkOffer(input)
	if err != nil {
		return err
	}
	offer.CreatedAt = time.Now().UTC()
	if err := s.offers.offers.Create(offer); err != nil {
		return err
	}
	return s.recordAdminAction(actor.ID, "admin.offer_created", "offer", offer.Email)
}

// DeleteOffer removes an offer nobody claimed yet, like one with a typo.
func (s *AdminService) DeleteOffer(actor *model.User, actorSession *model.Session, email string) error {
	if !s.IsAdmin(actor, actorSession) {
		return ErrAdminPermission
	}
	if s.offers == nil {
		return ErrNoWorkspace
	}
	email = offerEmail(email)
	if err := s.offers.offers.DeleteOpen(email); err != nil {
		return err
	}
	return s.recordAdminAction(actor.ID, "admin.offer_deleted", "offer", email)
}

// SetUserNote keeps what admins write about a user. An empty note removes
// it. The log says that it changed, never what it says.
func (s *AdminService) SetUserNote(actor *model.User, actorSession *model.Session, targetUserID, note string) error {
	if !s.IsAdmin(actor, actorSession) {
		return ErrAdminPermission
	}
	if _, err := s.users.ByID(targetUserID); err != nil {
		return err
	}
	note = strings.TrimSpace(strings.ReplaceAll(note, "\r\n", "\n"))
	if utf8.RuneCountInString(note) > model.UserNoteMax {
		return fmt.Errorf("%w: a note has at most %d characters", ErrAdminInput, model.UserNoteMax)
	}
	if err := s.repository.SetUserNote(targetUserID, note, actor.ID, time.Now().UTC()); err != nil {
		return err
	}
	return s.recordAdminAction(actor.ID, "admin.note_saved", "user", targetUserID)
}

// checkOffer turns what an admin typed into an offer, or says what is wrong.
func checkOffer(input OfferInput) (*model.Offer, error) {
	email := offerEmail(input.Email)
	if address, err := mail.ParseAddress(email); err != nil || address.Address != email {
		return nil, fmt.Errorf("%w: the email is not an address", ErrAdminInput)
	}
	if input.TrialDays < 1 || input.TrialDays > model.OfferMaxDays {
		return nil, fmt.Errorf("%w: days are 1 to %d", ErrAdminInput, model.OfferMaxDays)
	}
	coupon := strings.TrimSpace(input.CouponID)
	if len(coupon) > 100 || strings.ContainsAny(coupon, " \t\n") {
		return nil, fmt.Errorf("%w: the coupon is one word, a coupon ID from Stripe", ErrAdminInput)
	}
	reason := strings.TrimSpace(input.Reason)
	if utf8.RuneCountInString(reason) > model.OfferMaxReason {
		return nil, fmt.Errorf("%w: the reason has at most %d characters", ErrAdminInput, model.OfferMaxReason)
	}
	return &model.Offer{Email: email, TrialDays: input.TrialDays, CouponID: coupon, Reason: reason}, nil
}
