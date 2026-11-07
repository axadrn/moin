-- +goose Up
-- ============================================================================
-- APPS
-- A compose project on one server. The name is the project name on the
-- server too, so it stays lowercase letters, digits and dashes. A server with
-- apps cannot be removed.
-- ============================================================================
CREATE TABLE IF NOT EXISTS apps (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    server_id TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, name)
);

CREATE INDEX IF NOT EXISTS idx_apps_server_id ON apps(server_id);

-- ============================================================================
-- DEPLOYS
-- One release of an app, numbered per app. A rollback has from_number and no
-- source. detected is the JSON the agent reported: kind, service, port,
-- databases and the x-moin block.
-- ============================================================================
CREATE TABLE IF NOT EXISTS deploys (
    id TEXT PRIMARY KEY,
    app_id TEXT NOT NULL,
    number INTEGER NOT NULL,
    status TEXT NOT NULL,
    source_key TEXT NOT NULL DEFAULT '',
    source_sha256 TEXT NOT NULL DEFAULT '',
    from_number INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    log TEXT NOT NULL DEFAULT '',
    detected TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP,
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE,
    UNIQUE (app_id, number)
);

-- ============================================================================
-- APP ENV
-- Env vars of an app. value is sealed with AES-256-GCM under SECRETS_KEY,
-- bound to app_id and key.
-- ============================================================================
CREATE TABLE IF NOT EXISTS app_env (
    app_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (app_id, key),
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS app_env;
DROP TABLE IF EXISTS deploys;
DROP INDEX IF EXISTS idx_apps_server_id;
DROP TABLE IF EXISTS apps;
