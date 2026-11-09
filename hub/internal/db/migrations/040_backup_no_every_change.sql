-- +goose Up
-- SQLite is copied on a schedule like Postgres: there is no Every change.
ALTER TABLE apps DROP COLUMN backup_every_change;

-- +goose Down
ALTER TABLE apps ADD COLUMN backup_every_change INTEGER NOT NULL DEFAULT 1;
