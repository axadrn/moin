package model

import "time"

// Offer is free days and a coupon that wait for one email address. Whoever
// signs up with it gets them on the first workspace they create, or an
// admin gives them to an account with another address. Claimed, it says
// who got it.
type Offer struct {
	Email              string     `db:"email"`
	TrialDays          int        `db:"trial_days"`
	CouponID           string     `db:"coupon_id"`
	Reason             string     `db:"reason"`
	ExpiresAt          *time.Time `db:"expires_at"`
	ClaimedAt          *time.Time `db:"claimed_at"`
	ClaimedUserID      *string    `db:"claimed_user_id"`
	ClaimedWorkspaceID *string    `db:"claimed_workspace_id"`
	CreatedAt          time.Time  `db:"created_at"`
	// ClaimedEmail is the address of the account that got it, when it
	// still exists.
	ClaimedEmail string `db:"claimed_email"`
}

// Open is whether the offer still waits: not claimed, not expired.
func (o Offer) Open(now time.Time) bool {
	return o.ClaimedAt == nil && (o.ExpiresAt == nil || o.ExpiresAt.After(now))
}

// OfferLimits keep an offer sane: at most ten years free.
const (
	OfferMaxDays   = 3650
	OfferMaxReason = 200
)

// AdminOfferPage is a page of offers, newest first, and how many wait.
type AdminOfferPage struct {
	Offers     []Offer
	Query      string
	Page       int
	TotalPages int
	Total      int
	Open       int
	// Coupons are the coupons offers use already, to pick one again.
	Coupons []string
}

// UserNote is what admins wrote about a user. Only admins see it.
type UserNote struct {
	UserID         string    `db:"user_id"`
	Note           string    `db:"note"`
	UpdatedBy      *string   `db:"updated_by"`
	UpdatedAt      time.Time `db:"updated_at"`
	UpdatedByEmail string    `db:"updated_by_email"`
}

// UserNoteMax is the longest note, in characters.
const UserNoteMax = 5000
