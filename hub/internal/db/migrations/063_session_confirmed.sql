-- +goose Up
-- When and how the person of a session last confirmed it is them: at
-- sign-in, with a passkey or with GitHub. Adding a passkey, approving a
-- connection and giving more rights need it from the last five minutes,
-- with the passkey for an account that has one, so nobody signs out for
-- them.
-- SQLite adds a column to a table with rows only with a fixed default:
-- the sign-in time of each session comes after.
ALTER TABLE sessions ADD COLUMN confirmed_at TIMESTAMP NOT NULL DEFAULT '1970-01-01 00:00:00';
ALTER TABLE sessions ADD COLUMN confirmed_with TEXT NOT NULL DEFAULT 'sign-in';
UPDATE sessions SET confirmed_at = created_at;

-- +goose Down
ALTER TABLE sessions DROP COLUMN confirmed_with;
ALTER TABLE sessions DROP COLUMN confirmed_at;
