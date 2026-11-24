-- +goose Up
-- Secrets live only on the server. The hub forwards a value once to the
-- daemon, which keeps env values in apps/<app>/app.env and the secrets of
-- the server in server.json, and keeps only names and dates. The sealed
-- values go: env values, registry passwords, bucket secret keys, the
-- Tailscale key and tunnel tokens. server_secrets says which server keeps
-- which secret: bucket/<bucket id>, registry/<host>, tailscale, tunnel.
-- The instant password is shown once and kept as a bcrypt hash.
ALTER TABLE app_env DROP COLUMN value;
ALTER TABLE registries DROP COLUMN password;
ALTER TABLE buckets DROP COLUMN secret_key;
ALTER TABLE integrations DROP COLUMN tailscale_key;
ALTER TABLE servers DROP COLUMN tunnel_token;
ALTER TABLE apps DROP COLUMN instant_password;
ALTER TABLE apps ADD COLUMN instant_password_hash TEXT NOT NULL DEFAULT '';

CREATE TABLE server_secrets (
    server_id TEXT NOT NULL,
    name TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (server_id, name),
    FOREIGN KEY (server_id) REFERENCES servers(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE server_secrets;
ALTER TABLE apps DROP COLUMN instant_password_hash;
ALTER TABLE apps ADD COLUMN instant_password TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN tunnel_token TEXT NOT NULL DEFAULT '';
ALTER TABLE integrations ADD COLUMN tailscale_key TEXT NOT NULL DEFAULT '';
ALTER TABLE buckets ADD COLUMN secret_key TEXT NOT NULL DEFAULT '';
ALTER TABLE registries ADD COLUMN password TEXT NOT NULL DEFAULT '';
ALTER TABLE app_env ADD COLUMN value TEXT NOT NULL DEFAULT '';
