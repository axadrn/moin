package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	// ErrOfferExists means an offer waits for that address already.
	ErrOfferExists = errors.New("an offer for this address exists already")
	// ErrOfferNotOpen means no open offer waits for that address.
	ErrOfferNotOpen = errors.New("no open offer for this address")
)

// Offers are free days and a coupon that wait for an email address.
type Offers struct {
	db *sqlx.DB
}

func NewOffers(db *sqlx.DB) *Offers {
	return &Offers{db: db}
}

// Claim marks the offer of email claimed by the user and applies it to the
// subscription of the workspace, both or neither. It answers false and
// changes nothing when no open offer waits for email, or when the workspace
// pays or is free forever: then the offer stays open.
func (r *Offers) Claim(email, userID, workspaceID string, now time.Time) (bool, error) {
	tx, err := r.db.Beginx()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	subscription := &model.Subscription{}
	if err := tx.Get(subscription, `SELECT * FROM subscriptions WHERE workspace_id = $1`, workspaceID); err != nil {
		return false, err
	}
	if subscription.HasProviderSubscription() || subscription.IsForeverFree() {
		return false, nil
	}

	var offer struct {
		TrialDays int    `db:"trial_days"`
		CouponID  string `db:"coupon_id"`
	}
	err = tx.Get(&offer, `
		UPDATE offers SET claimed_at = $1, claimed_user_id = $2, claimed_workspace_id = $3
		WHERE email = $4 AND claimed_at IS NULL AND (expires_at IS NULL OR expires_at > $1)
		RETURNING trial_days, coupon_id
	`, now, userID, workspaceID, email)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	subscription.StartOffer(offer.TrialDays, offer.CouponID, now)
	if _, err := tx.Exec(`
		UPDATE subscriptions
		SET plan_id = $1, status = $2, current_period_end = $3, coupon_id = $4, updated_at = $5
		WHERE id = $6
	`, subscription.PlanID, subscription.Status, subscription.CurrentPeriodEnd, subscription.CouponID, subscription.UpdatedAt, subscription.ID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// UsersWithOpenOffer are the accounts whose address has an open offer.
func (r *Offers) UsersWithOpenOffer(now time.Time) ([]model.User, error) {
	users := []model.User{}
	err := r.db.Select(&users, `
		SELECT users.* FROM users
		JOIN offers ON offers.email = lower(trim(users.email))
		WHERE offers.claimed_at IS NULL AND (offers.expires_at IS NULL OR offers.expires_at > $1)
	`, now)
	return users, err
}

// Create adds an offer; ErrOfferExists when one has that address, open or
// claimed.
func (r *Offers) Create(offer *model.Offer) error {
	result, err := r.db.Exec(`
		INSERT INTO offers (email, trial_days, coupon_id, reason, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email) DO NOTHING
	`, offer.Email, offer.TrialDays, offer.CouponID, offer.Reason, offer.ExpiresAt, offer.CreatedAt)
	if err != nil {
		return err
	}
	if added, err := result.RowsAffected(); err != nil || added == 0 {
		return errors.Join(ErrOfferExists, err)
	}
	return nil
}

// DeleteOpen removes the offer of email while nobody claimed it;
// ErrOfferNotOpen otherwise. A claimed offer stays: it says who got what.
func (r *Offers) DeleteOpen(email string) error {
	result, err := r.db.Exec(`DELETE FROM offers WHERE email = $1 AND claimed_at IS NULL`, email)
	if err != nil {
		return err
	}
	if removed, err := result.RowsAffected(); err != nil || removed == 0 {
		return errors.Join(ErrOfferNotOpen, err)
	}
	return nil
}

// offerSelect is an offer with the address of the account that got it.
const offerSelect = `
	SELECT offers.*, COALESCE(users.email, '') AS claimed_email
	FROM offers LEFT JOIN users ON users.id = offers.claimed_user_id
`

// List is a page of offers, newest first. query finds the address an offer
// waits for and the address of the account that got it.
func (r *Offers) List(query string, limit, offset int) ([]model.Offer, int, error) {
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	where := `WHERE ($1 = '%%' OR offers.email LIKE $1 OR LOWER(users.email) LIKE $1 OR LOWER(offers.reason) LIKE $1)`
	var total int
	if err := r.db.Get(&total, `SELECT COUNT(*) FROM offers LEFT JOIN users ON users.id = offers.claimed_user_id `+where, pattern); err != nil {
		return nil, 0, err
	}
	offers := []model.Offer{}
	err := r.db.Select(&offers, offerSelect+where+`
		ORDER BY offers.created_at DESC, offers.email
		LIMIT $2 OFFSET $3
	`, pattern, limit, offset)
	return offers, total, err
}

// CountOpen is how many offers still wait.
func (r *Offers) CountOpen(now time.Time) (int, error) {
	var open int
	err := r.db.Get(&open, `SELECT COUNT(*) FROM offers WHERE claimed_at IS NULL AND (expires_at IS NULL OR expires_at > $1)`, now)
	return open, err
}

// Coupons are the coupons offers use, most used first.
func (r *Offers) Coupons() ([]string, error) {
	coupons := []string{}
	err := r.db.Select(&coupons, `
		SELECT coupon_id FROM offers WHERE coupon_id != ''
		GROUP BY coupon_id ORDER BY COUNT(*) DESC, coupon_id LIMIT 10
	`)
	return coupons, err
}

// ForUser is the offer the user got, or else the one that waits for their
// address; nil when there is neither.
func (r *Offers) ForUser(userID, email string) (*model.Offer, error) {
	var offer model.Offer
	err := r.db.Get(&offer, offerSelect+`
		WHERE offers.claimed_user_id = $1 OR (offers.claimed_at IS NULL AND offers.email = $2)
		ORDER BY offers.claimed_at IS NULL, offers.claimed_at DESC
		LIMIT 1
	`, userID, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &offer, nil
}
