-- +goose Up
-- The GitHub App an admin of a self-hosted hub created with one click,
-- from a manifest. One row at most. GITHUB_APP_* in the env wins over it.
-- The client secret, the webhook secret and the private key are sealed
-- with SECRETS_KEY.
CREATE TABLE IF NOT EXISTS github_app (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    app_id TEXT NOT NULL,
    slug TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    webhook_secret TEXT NOT NULL,
    private_key TEXT NOT NULL,
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS github_app;
