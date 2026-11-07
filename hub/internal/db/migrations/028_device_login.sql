-- +goose Up
-- ============================================================================
-- DEVICE LOGIN: `moin login` IN THE BROWSER
-- kind tells a personal API token made in Settings from a device token that
-- `moin login` got through the browser. A device token expires 90 days
-- after its last use; each use moves expires_at.
-- device_logins are the pending logins of the device flow (RFC 8628): the
-- CLI holds the device code, the user opens the user code on the web and
-- approves or denies it. Only hashes of both codes are stored. A login lives
-- ten minutes and is deleted once the CLI has its token.
-- ============================================================================
ALTER TABLE api_tokens ADD COLUMN kind TEXT NOT NULL DEFAULT 'personal' CHECK (kind IN ('personal', 'device'));

CREATE TABLE IF NOT EXISTS device_logins (
    id TEXT PRIMARY KEY,
    device_code_hash TEXT UNIQUE NOT NULL,
    user_code_hash TEXT UNIQUE NOT NULL,
    hostname TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied')),
    user_id TEXT,
    workspace_id TEXT,
    interval_seconds INTEGER NOT NULL,
    last_polled_at TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_device_logins_expires_at ON device_logins(expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_device_logins_expires_at;
DROP TABLE IF EXISTS device_logins;
ALTER TABLE api_tokens DROP COLUMN kind;
