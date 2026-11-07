-- +goose Up
-- ============================================================================
-- BACKUP AND DISK ALERTS
-- backup_status keeps the newest restore check's error apart from the
-- backup's, so a failed check and broken backups alert on their own.
-- disk_alerts holds the servers whose disk is over 90%: one alert on the
-- way up, one when it is under 85% again, and none twice across restarts.
-- ============================================================================
ALTER TABLE backup_status ADD COLUMN drill_error TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS disk_alerts (
    server_id TEXT PRIMARY KEY,
    since TIMESTAMP NOT NULL,
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);

-- Destinations that got every event get the new ones too.
UPDATE alert_destinations
SET events = events || ',backup.failed,backup.recovered,drill.failed,drill.recovered,disk.full,disk.ok'
WHERE events = 'uptime.down,uptime.up,server.down,server.up,deploy.failed';

-- +goose Down
DROP TABLE IF EXISTS disk_alerts;
ALTER TABLE backup_status DROP COLUMN drill_error;
