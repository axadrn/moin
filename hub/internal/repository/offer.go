package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/jmoiron/sqlx"
)

// Offers are free days and a coupon that wait for an email address.
type Offers struct {
	db *sqlx.DB
}

func NewOffers(db *sqlx.DB) *Offers {
	return &Offers{db: db}
}

// Claim marks the offer of email claimed and applies it to the subscription
// of the workspace, both or neither. It answers false and changes nothing
// when no open offer waits for email, or when the workspace pays or is free
// forever: then the offer stays open.
func (r *Offers) Claim(email, workspaceID string, now time.Time) (bool, error) {
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
		UPDATE offers SET claimed_at = $1
		WHERE email = $2 AND claimed_at IS NULL AND (expires_at IS NULL OR expires_at > $3)
		RETURNING trial_days, coupon_id
	`, now, email, now)
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
