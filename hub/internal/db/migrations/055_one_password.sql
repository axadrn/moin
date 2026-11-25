-- +goose Up
-- An app has one password: protect, an htpasswd line, empty when off.
-- When it is on, every address of the app asks for it, its domains and
-- its instant address. An app whose main service had no domain, only the
-- guarded instant address, stays guarded: it keeps the password moin made
-- as its own, or, when moin kept none, one nobody knows, so it fails
-- closed until you set a new one. An app whose main service had domains
-- and no password of its own stays off: those domains were open already.
UPDATE apps SET protect = 'moin:' || CASE WHEN instant_password_hash != '' THEN instant_password_hash ELSE '!' END
WHERE protect = '' AND instant_open = 0
  AND NOT EXISTS (SELECT 1 FROM domains WHERE domains.app_id = apps.id AND domains.service = '');

ALTER TABLE apps DROP COLUMN instant_open;
ALTER TABLE apps DROP COLUMN instant_password_hash;

-- +goose Down
ALTER TABLE apps ADD COLUMN instant_password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN instant_open INTEGER NOT NULL DEFAULT 0;
