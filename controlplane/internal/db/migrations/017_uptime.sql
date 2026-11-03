-- +goose Up
-- ============================================================================
-- UPTIME
-- The control plane checks every domain of every live app once a minute,
-- like a visitor, and keeps the newest results in memory. uptime_checks
-- holds what must survive a restart: the settings of a domain (a path,
-- empty for the health path or /, and paused) and its state, up or down,
-- with since when. It is written when a setting or the state changes,
-- never per check.
-- ============================================================================
CREATE TABLE IF NOT EXISTS uptime_checks (
    app_id TEXT NOT NULL,
    domain TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '',
    paused INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT '',
    since TIMESTAMP,
    PRIMARY KEY (app_id, domain),
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
);

-- One row per domain and hour: how many checks ran, how many found it up,
-- their response times added up and the slowest. The checker adds its
-- counts in one transaction every few minutes; 24 hours, 7 and 30 days add
-- these up. Rows older than 31 days go.
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

-- An outage of a domain: from the check that found it down, after a retry,
-- to the one that found it up again; ended_at is NULL while it lasts.
-- reason is why, such as "HTTP 502" or "no answer within 10s". Ended
-- outages older than 90 days go.
CREATE TABLE IF NOT EXISTS uptime_outages (
    app_id TEXT NOT NULL,
    domain TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    ended_at TIMESTAMP,
    reason TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (app_id, domain, started_at),
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
);

-- A server whose agent stopped answering: from when it was last seen to
-- when it connected again.
CREATE TABLE IF NOT EXISTS server_outages (
    server_id TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    ended_at TIMESTAMP,
    PRIMARY KEY (server_id, started_at),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS server_outages;
DROP TABLE IF EXISTS uptime_outages;
DROP INDEX IF EXISTS idx_uptime_hours_hour;
DROP TABLE IF EXISTS uptime_hours;
DROP TABLE IF EXISTS uptime_checks;
