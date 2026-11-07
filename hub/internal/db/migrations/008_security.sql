-- +goose Up
-- ============================================================================
-- SECURITY
-- How a server is reached and locked down. With tunnel on, a Cloudflare
-- Tunnel created with the user's own API token carries all app traffic and
-- no port is open; tunnel_token is sealed. ssh_tailscale answers SSH only
-- on the user's tailnet, firewall drops every other inbound connection.
-- security is what the agent last reported, as JSON. integrations holds
-- the user's Cloudflare API token and Tailscale auth key, both sealed.
-- ============================================================================
ALTER TABLE servers ADD COLUMN tunnel INTEGER NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN tunnel_id TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN tunnel_token TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN ssh_tailscale INTEGER NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN firewall INTEGER NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN security TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS integrations (
    workspace_id TEXT PRIMARY KEY,
    cloudflare_token TEXT NOT NULL DEFAULT '',
    cloudflare_account TEXT NOT NULL DEFAULT '',
    tailscale_key TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS integrations;
ALTER TABLE servers DROP COLUMN security;
ALTER TABLE servers DROP COLUMN firewall;
ALTER TABLE servers DROP COLUMN ssh_tailscale;
ALTER TABLE servers DROP COLUMN tunnel_token;
ALTER TABLE servers DROP COLUMN tunnel_id;
ALTER TABLE servers DROP COLUMN tunnel;
