-- +goose Up
-- ============================================================================
-- CATALOG
-- An app installed from the catalog keeps the slug of its entry, such as
-- umami, so ops can offer its newer versions. Its version is the env var
-- VERSION; empty is an app of your own.
-- ============================================================================
ALTER TABLE apps ADD COLUMN catalog TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE apps DROP COLUMN catalog;
