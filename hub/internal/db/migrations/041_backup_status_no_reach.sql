-- +goose Up
-- A backup is copies on a schedule: no more reach of Every change.
ALTER TABLE backup_status DROP COLUMN since;
ALTER TABLE backup_status DROP COLUMN replicated_at;

-- +goose Down
ALTER TABLE backup_status ADD COLUMN since TIMESTAMP;
ALTER TABLE backup_status ADD COLUMN replicated_at TIMESTAMP;
