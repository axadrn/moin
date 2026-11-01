-- +goose Up
-- ============================================================================
-- ROUTING
-- How visitors reach an app: its domains (a JSON array), the canonical one
-- the others redirect to, and an htpasswd line ("user:bcrypt hash") when a
-- password protects it. The x-ops block of compose.yml wins for every field
-- it sets. servers.address is the IP the agent connected from, for the DNS
-- check.
-- ============================================================================
ALTER TABLE apps ADD COLUMN domains TEXT NOT NULL DEFAULT '[]';
ALTER TABLE apps ADD COLUMN canonical TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN protect TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN address TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN address;
ALTER TABLE apps DROP COLUMN protect;
ALTER TABLE apps DROP COLUMN canonical;
ALTER TABLE apps DROP COLUMN domains;
