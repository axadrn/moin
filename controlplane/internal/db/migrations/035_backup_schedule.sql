-- +goose Up
-- A backup is a copy of the database on a schedule: every backup_every
-- minutes, each copy kept backup_keep days. backup_every_change also
-- records every change of a SQLite file, so a restore can go back to any
-- minute. copied_at is the newest copy the agent reported.
ALTER TABLE apps ADD COLUMN backup_every INTEGER NOT NULL DEFAULT 60;
ALTER TABLE apps ADD COLUMN backup_keep INTEGER NOT NULL DEFAULT 7;
ALTER TABLE apps ADD COLUMN backup_every_change INTEGER NOT NULL DEFAULT 1;
ALTER TABLE backup_status ADD COLUMN copied_at TIMESTAMP;

-- +goose Down
ALTER TABLE backup_status DROP COLUMN copied_at;
ALTER TABLE apps DROP COLUMN backup_every_change;
ALTER TABLE apps DROP COLUMN backup_keep;
ALTER TABLE apps DROP COLUMN backup_every;
