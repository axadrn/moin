-- +goose Up
-- ============================================================================
-- CATALOG
-- An app installed from the catalog keeps the slug of its entry, such as
-- umami, so ops can offer its newer versions. Its version is the env var
-- VERSION; empty is an app of your own. catalog_notified is the newest
-- release an update.available alert went out for, so each version alerts
-- once, also across restarts.
-- ============================================================================
ALTER TABLE apps ADD COLUMN catalog TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN catalog_notified TEXT NOT NULL DEFAULT '';

-- Destinations that got every event get update.available too.
UPDATE alert_destinations
SET events = events || ',update.available'
WHERE events = 'uptime.down,uptime.up,server.down,server.up,deploy.failed,backup.failed,backup.recovered,drill.failed,drill.recovered,disk.full,disk.ok';

-- +goose Down
UPDATE alert_destinations
SET events = REPLACE(events, ',update.available', '');
ALTER TABLE apps DROP COLUMN catalog_notified;
ALTER TABLE apps DROP COLUMN catalog;
