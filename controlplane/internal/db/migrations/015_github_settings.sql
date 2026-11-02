-- +goose Up
-- Where on github.com the owner of an installation chooses the repos ops
-- may read: the account's or the organization's settings page for it.
ALTER TABLE github_installations ADD COLUMN settings_url TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE github_installations DROP COLUMN settings_url;
