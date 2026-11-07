-- +goose Up
-- ============================================================================
-- BUCKETS
-- A workspace keeps a list of named buckets instead of one. The bucket it had
-- becomes its default, named "default", with the same sealed secret key, so
-- nothing changes for its apps. Each app may pick a bucket; NULL follows the
-- workspace's default. app_buckets remembers the buckets an app left: its
-- older backups stay there, restores read them from there, and such a
-- bucket cannot be removed while the app exists.
-- ============================================================================
CREATE TABLE IF NOT EXISTS buckets (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    endpoint TEXT NOT NULL,
    region TEXT NOT NULL,
    bucket TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '',
    access_key TEXT NOT NULL,
    secret_key TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (workspace_id, name),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_buckets_default ON buckets(workspace_id) WHERE is_default = 1;

INSERT INTO buckets (id, workspace_id, name, is_default, endpoint, region, bucket, path, access_key, secret_key, created_at)
SELECT lower(hex(randomblob(16))), workspace_id, 'default', 1, endpoint, region, bucket, path, access_key, secret_key, created_at
FROM backup_storage;

DROP TABLE backup_storage;

ALTER TABLE apps ADD COLUMN bucket_id TEXT;

CREATE TABLE IF NOT EXISTS app_buckets (
    app_id TEXT NOT NULL,
    bucket_id TEXT NOT NULL,
    left_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (app_id, bucket_id),
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_app_buckets_bucket ON app_buckets(bucket_id);

-- +goose Down
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
INSERT INTO backup_storage (workspace_id, endpoint, region, bucket, path, access_key, secret_key, created_at)
SELECT workspace_id, endpoint, region, bucket, path, access_key, secret_key, created_at FROM buckets WHERE is_default = 1;
DROP TABLE IF EXISTS app_buckets;
ALTER TABLE apps DROP COLUMN bucket_id;
DROP TABLE IF EXISTS buckets;
