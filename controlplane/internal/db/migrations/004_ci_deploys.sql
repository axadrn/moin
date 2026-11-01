-- +goose Up
-- ============================================================================
-- CI DEPLOYS
-- An image deploy runs an image built elsewhere, such as in CI. The deploy
-- hook is a secret URL per app that starts one; only its SHA-256 hash is
-- stored. Registry logins let agents pull private images; the password is
-- sealed with AES-256-GCM under SECRETS_KEY, bound to user and host.
-- ============================================================================
ALTER TABLE apps ADD COLUMN deploy_hook_hash TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_apps_deploy_hook_hash ON apps(deploy_hook_hash);

ALTER TABLE deploys ADD COLUMN image TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS registries (
    workspace_id TEXT NOT NULL,
    host TEXT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (workspace_id, host),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS registries;
DROP INDEX IF EXISTS idx_apps_deploy_hook_hash;
ALTER TABLE deploys DROP COLUMN image;
ALTER TABLE apps DROP COLUMN deploy_hook_hash;
