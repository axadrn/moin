-- +goose Up
-- ============================================================================
-- TELEMETRY
-- instance holds the one random ID of this control plane; a self-hosted one
-- sends it every five minutes with a few counts, unless TELEMETRY=false.
-- telemetry keeps the newest report of every self-hosted control plane, on
-- the hosted one: only counts, never a name, a domain or an address.
-- ============================================================================
CREATE TABLE IF NOT EXISTS instance (
    id TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS telemetry (
    instance_id TEXT PRIMARY KEY,
    version TEXT NOT NULL DEFAULT '',
    servers INTEGER NOT NULL DEFAULT 0,
    apps INTEGER NOT NULL DEFAULT 0,
    deploys INTEGER NOT NULL DEFAULT 0,
    seen_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_telemetry_seen_at ON telemetry(seen_at);

-- +goose Down
DROP TABLE IF EXISTS telemetry;
DROP TABLE IF EXISTS instance;
