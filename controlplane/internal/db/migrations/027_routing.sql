-- +goose Up
-- ============================================================================
-- ROUTING: DOMAINS PER SERVICE AND THE INSTANT ADDRESS
-- service_domains are the domains of an app's services other than the main
-- one, a JSON object of service to domains; domains stays the main one's.
-- instant is the provider of the app's instant address, such as sslip.io,
-- or off; instant_host the address the last deploy served it at, empty
-- when none. A password guards it unless instant_open: the app's own one
-- when it has one, else instant_password, made by ops and sealed.
-- ============================================================================
ALTER TABLE apps ADD COLUMN service_domains TEXT NOT NULL DEFAULT '{}';
ALTER TABLE apps ADD COLUMN instant TEXT NOT NULL DEFAULT 'sslip.io';
ALTER TABLE apps ADD COLUMN instant_host TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN instant_open INTEGER NOT NULL DEFAULT 0;
ALTER TABLE apps ADD COLUMN instant_password TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE apps DROP COLUMN instant_password;
ALTER TABLE apps DROP COLUMN instant_open;
ALTER TABLE apps DROP COLUMN instant_host;
ALTER TABLE apps DROP COLUMN instant;
ALTER TABLE apps DROP COLUMN service_domains;
