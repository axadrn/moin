-- +goose Up
-- Whether the user gets a mail when a site of a workspace they own or run as
-- admin goes down or comes back, a server goes offline or comes back, or a
-- deploy fails. On by default.
ALTER TABLE users ADD COLUMN email_alerts INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE users DROP COLUMN email_alerts;
