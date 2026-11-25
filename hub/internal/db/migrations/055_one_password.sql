-- +goose Up
-- An app has one password: protect, an htpasswd line, empty when off.
-- When it is on, every address of the app asks for it, its domains and
-- its instant address. An app whose instant address had the password moin
-- made, and no domains, keeps that password as its own, so it keeps
-- working. An app with domains and no password of its own stays off: its
-- domains were open, and its instant address opens with them.
UPDATE apps SET protect = 'moin:' || instant_password_hash
WHERE protect = '' AND instant_open = 0 AND instant_password_hash != ''
  AND NOT EXISTS (SELECT 1 FROM domains WHERE domains.app_id = apps.id);

ALTER TABLE apps DROP COLUMN instant_open;
ALTER TABLE apps DROP COLUMN instant_password_hash;

-- +goose Down
ALTER TABLE apps ADD COLUMN instant_password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN instant_open INTEGER NOT NULL DEFAULT 0;
