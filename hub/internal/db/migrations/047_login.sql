-- +goose Up
-- Sign-in is GitHub only. github_id is the GitHub account of a user; after
-- the first sign-in only it counts, never the email. github_login is its
-- name on GitHub, kept fresh on every sign-in. A recovery key comes from the
-- server command recovery-key: only its SHA-256 is stored, it works once and
-- for an hour. Passwords and sign-in links are gone, and so is every web
-- session: everyone signs in again, once, with GitHub.
ALTER TABLE users ADD COLUMN github_id INTEGER;
ALTER TABLE users ADD COLUMN github_login TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN recovery_key_hash TEXT;
ALTER TABLE users ADD COLUMN recovery_key_expires_at TIMESTAMP;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_github_id ON users(github_id) WHERE github_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_recovery_key_hash ON users(recovery_key_hash) WHERE recovery_key_hash IS NOT NULL;

DROP INDEX IF EXISTS idx_users_passwordless;
ALTER TABLE users DROP COLUMN password_hash;
DELETE FROM tokens WHERE type IN ('magic_link', 'password_reset');
DELETE FROM sessions;

-- +goose Down
ALTER TABLE users ADD COLUMN password_hash TEXT NULL;
CREATE INDEX IF NOT EXISTS idx_users_passwordless ON users(id) WHERE password_hash IS NULL;
DROP INDEX IF EXISTS idx_users_recovery_key_hash;
DROP INDEX IF EXISTS idx_users_github_id;
ALTER TABLE users DROP COLUMN recovery_key_expires_at;
ALTER TABLE users DROP COLUMN recovery_key_hash;
ALTER TABLE users DROP COLUMN github_login;
ALTER TABLE users DROP COLUMN github_id;
