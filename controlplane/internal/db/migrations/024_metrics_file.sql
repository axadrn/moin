-- +goose Up
-- ============================================================================
-- METRICS MOVE TO metrics.db
-- Metrics are history the agents send again every 15 seconds. They now live
-- in their own file next to this database, without a backup, so this one
-- stays small, its backup cheap and a restore fast. The few hours of
-- metrics kept here so far go; new ones start in metrics.db.
-- ============================================================================
DROP INDEX IF EXISTS idx_metrics_hours_hour;
DROP TABLE IF EXISTS metrics_hours;
DROP INDEX IF EXISTS idx_metrics_minutes_minute;
DROP TABLE IF EXISTS metrics_minutes;

-- +goose Down
CREATE TABLE IF NOT EXISTS metrics_minutes (
    target TEXT NOT NULL,
    minute INTEGER NOT NULL,
    cpu INTEGER NOT NULL DEFAULT 0,
    memory INTEGER NOT NULL DEFAULT 0,
    disk INTEGER NOT NULL DEFAULT 0,
    net_rx INTEGER NOT NULL DEFAULT 0,
    net_tx INTEGER NOT NULL DEFAULT 0,
    load INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (target, minute)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_metrics_minutes_minute ON metrics_minutes(minute);

CREATE TABLE IF NOT EXISTS metrics_hours (
    target TEXT NOT NULL,
    hour INTEGER NOT NULL,
    cpu INTEGER NOT NULL DEFAULT 0,
    memory INTEGER NOT NULL DEFAULT 0,
    disk INTEGER NOT NULL DEFAULT 0,
    net_rx INTEGER NOT NULL DEFAULT 0,
    net_tx INTEGER NOT NULL DEFAULT 0,
    load INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (target, hour)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_metrics_hours_hour ON metrics_hours(hour);

