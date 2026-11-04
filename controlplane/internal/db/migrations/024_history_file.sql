-- +goose Up
-- ============================================================================
-- HISTORY MOVES TO history.db
-- Metrics and the hourly uptime counts are history: charts that grow back
-- by themselves. They now live in history.db next to this database, without
-- a backup, so this one stays small, its backup cheap and a restore fast.
-- What was kept here so far goes once; new history starts in history.db.
-- The uptime settings, state and outages stay here: alerts depend on them.
-- ============================================================================
DROP INDEX IF EXISTS idx_metrics_hours_hour;
DROP TABLE IF EXISTS metrics_hours;
DROP INDEX IF EXISTS idx_metrics_minutes_minute;
DROP TABLE IF EXISTS metrics_minutes;
DROP INDEX IF EXISTS idx_uptime_hours_hour;
DROP TABLE IF EXISTS uptime_hours;

-- +goose Down
CREATE TABLE IF NOT EXISTS uptime_hours (
    app_id TEXT NOT NULL,
    domain TEXT NOT NULL,
    hour TIMESTAMP NOT NULL,
    checks INTEGER NOT NULL DEFAULT 0,
    up INTEGER NOT NULL DEFAULT 0,
    response_ms INTEGER NOT NULL DEFAULT 0,
    max_ms INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (app_id, domain, hour),
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_uptime_hours_hour ON uptime_hours(hour);

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

