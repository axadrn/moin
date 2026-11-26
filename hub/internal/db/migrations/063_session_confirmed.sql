-- +goose Up
-- When the person of a session last confirmed it is them: at sign-in, with
-- a passkey or with GitHub. Adding a passkey and approving a connection
-- need it from the last five minutes, so nobody signs out for them.
ALTER TABLE sessions ADD COLUMN confirmed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP;
UPDATE sessions SET confirmed_at = created_at;

-- +goose Down
ALTER TABLE sessions DROP COLUMN confirmed_at;
