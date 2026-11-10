-- +goose Up
-- An offer waits for an email address: free days and a coupon for the first
-- workspace of that address. The coupon moves to the subscription on claim.
CREATE TABLE offers (
    email TEXT PRIMARY KEY,
    trial_days INTEGER NOT NULL,
    coupon_id TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMP,
    claimed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);
ALTER TABLE subscriptions ADD COLUMN coupon_id TEXT;

-- +goose Down
ALTER TABLE subscriptions DROP COLUMN coupon_id;
DROP TABLE offers;
