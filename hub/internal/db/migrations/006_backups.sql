-- +goose Up
-- ============================================================================
-- BACKUPS
-- The agent backs up every SQLite database of an app into the user's own
-- S3-compatible bucket: continuously with Litestream and as snapshots. One
-- bucket per account; its secret key is sealed with AES-256-GCM under
-- SECRETS_KEY, bound to the user. snapshot_before_changes takes a snapshot
-- before every change made through moin. backup_status keeps what the agent
-- last reported per database: the replication window and the newest
-- restore drill.
-- ============================================================================
CREATE TABLE IF NOT EXISTS backup_storage (
    workspace_id TEXT PRIMARY KEY,
    endpoint TEXT NOT NULL,
    region TEXT NOT NULL,
    bucket TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '',
    access_key TEXT NOT NULL,
    secret_key TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

ALTER TABLE apps ADD COLUMN snapshot_before_changes INTEGER NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS backup_status (
    app_id TEXT NOT NULL,
    database TEXT NOT NULL,
    since TIMESTAMP,
    replicated_at TIMESTAMP,
    verified_at TIMESTAMP,
    error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (app_id, database),
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS backup_status;
ALTER TABLE apps DROP COLUMN snapshot_before_changes;
DROP TABLE IF EXISTS backup_storage;
