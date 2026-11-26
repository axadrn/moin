-- +goose Up
-- When and how the person of a session last confirmed it is them: at
-- sign-in, with a passkey or with GitHub. Adding a passkey, approving a
-- connection and giving more rights need it from the last five minutes,
-- with the passkey for an account that has one, so nobody signs out for
-- them.
ALTER TABLE sessions ADD COLUMN confirmed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE sessions ADD COLUMN confirmed_with TEXT NOT NULL DEFAULT 'sign-in';
UPDATE sessions SET confirmed_at = created_at;

-- +goose Down
ALTER TABLE sessions DROP COLUMN confirmed_with;
ALTER TABLE sessions DROP COLUMN confirmed_at;
