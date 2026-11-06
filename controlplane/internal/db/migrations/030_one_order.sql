-- +goose Up
-- One order for everyone: what needs you, what deploys, pinned apps, then
-- by name. Nobody picks a sort anymore.
ALTER TABLE users DROP COLUMN app_sort;

-- +goose Down
ALTER TABLE users ADD COLUMN app_sort TEXT NOT NULL DEFAULT 'smart';
