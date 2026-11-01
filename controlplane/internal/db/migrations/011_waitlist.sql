-- +goose Up
-- ============================================================================
-- WAITLIST
-- With sign-up closed (SIGNUPS_OPEN=false), new addresses land here. An admin
-- lets someone in: let_in_at is set and their first workspace gets
-- everything unlocked as a beta workspace; forever_free marks the ones that
-- stay free when billing starts for everyone else.
-- ============================================================================
CREATE TABLE IF NOT EXISTS waitlist (
    email TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    let_in_at TIMESTAMP,
    forever_free INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE IF EXISTS waitlist;
