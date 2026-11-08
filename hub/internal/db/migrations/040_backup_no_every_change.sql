-- +goose Up
-- SQLite always writes down every change now: there is no switch.
ALTER TABLE apps DROP COLUMN backup_every_change;

-- +goose Down
ALTER TABLE apps ADD COLUMN backup_every_change INTEGER NOT NULL DEFAULT 1;
