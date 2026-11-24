-- +goose Up
-- from_compose says x-moin.domains in the app's compose.yml set this
-- domain, not someone in moin. If compose.yml has domains, exactly those
-- count. Otherwise the ones entered in moin. Never a mix. Each deploy
-- makes the app's rows match its compose.yml before the release starts.
ALTER TABLE domains ADD COLUMN from_compose INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE domains DROP COLUMN from_compose;
