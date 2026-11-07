-- +goose Up
-- ============================================================================
-- METRICS
-- Every agent sends a sample of its server and apps every 15 seconds. The
-- control plane keeps the last hour of samples in memory and writes one row
-- per server or app and minute here, all in one transaction a minute, never
-- a row per sample. Once an hour adds up into metrics_hours.
--
-- target is a server ID or an app ID. Minutes and hours are Unix seconds
-- at their start. Values are integers, which SQLite stores in few bytes:
-- cpu in tenths of a percent, memory and disk in bytes, net_rx and net_tx
-- in bytes per second, load in hundredths. Apps leave disk and load at 0.
-- Minutes go after 24 hours, hours after 30 days.
-- ============================================================================
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

-- +goose Down
DROP INDEX IF EXISTS idx_metrics_hours_hour;
DROP TABLE IF EXISTS metrics_hours;
DROP INDEX IF EXISTS idx_metrics_minutes_minute;
DROP TABLE IF EXISTS metrics_minutes;
