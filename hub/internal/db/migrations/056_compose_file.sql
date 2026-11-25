-- +goose Up
-- The compose file of a deploy that worked, as its daemon made it: what
-- runs for the app, written to run anywhere, without a secret value.
-- Empty for a deploy before it or from an older daemon.
ALTER TABLE deploys ADD COLUMN compose_file TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deploys DROP COLUMN compose_file;
