-- +goose Up
-- The version a catalog app runs. Env values are write-only, so moin keeps
-- the version it gives the env var VERSION here too; compose.yml still
-- picks the image tag with VERSION. Apps installed before start empty and
-- get it with their next update.
ALTER TABLE apps ADD COLUMN catalog_version TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE apps DROP COLUMN catalog_version;
