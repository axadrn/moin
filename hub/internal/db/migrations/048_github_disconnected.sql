-- +goose Up
-- When a recovery key last disconnected the user's GitHub account. Such an
-- account connects GitHub again only from its settings, never by its email:
-- the old GitHub account, maybe someone else's now, has the same email.
ALTER TABLE users ADD COLUMN github_disconnected_at TIMESTAMP;

-- +goose Down
ALTER TABLE users DROP COLUMN github_disconnected_at;
