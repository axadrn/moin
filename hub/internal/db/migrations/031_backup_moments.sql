-- +goose Up
-- A backup is one history of every change, 7 days long: no more copies
-- before changes, so the setting for them goes. Instead moin remembers
-- the moments before a deploy, a data edit and a catalog update, by the
-- server's clock, so a restore can go back to them. database is empty for
-- a moment of every database of the app, such as a deploy.
ALTER TABLE apps DROP COLUMN snapshot_before_changes;

CREATE TABLE IF NOT EXISTS backup_moments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    app_id TEXT NOT NULL,
    database TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS backup_moments_app ON backup_moments (app_id, at);

-- +goose Down
DROP TABLE IF EXISTS backup_moments;
ALTER TABLE apps ADD COLUMN snapshot_before_changes INTEGER NOT NULL DEFAULT 1;
