-- +goose Up
-- Passkeys: a second way in, next to GitHub. credential is the JSON of
-- go-webauthn's webauthn.Credential: its public key, sign counter and
-- flags. credential_id is its id, which the browser sends back on sign-in.
CREATE TABLE IF NOT EXISTS passkeys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BLOB NOT NULL UNIQUE,
    credential TEXT NOT NULL,
    name TEXT NOT NULL,
    last_used_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_passkeys_user_id ON passkeys(user_id);

-- A passkey ceremony between its options and the browser's answer: the
-- JSON of go-webauthn's webauthn.SessionData, with the challenge. It is
-- kept by the SHA-256 of a random id in a cookie, used once, for five
-- minutes.
CREATE TABLE IF NOT EXISTS passkey_ceremonies (
    id_hash TEXT PRIMARY KEY,
    session_data TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS passkey_ceremonies;
DROP INDEX IF EXISTS idx_passkeys_user_id;
DROP TABLE IF EXISTS passkeys;
