-- +goose Up
-- Who SSH answers on a server: tailscale (only through Tailscale), everyone, or off
-- (sshd stopped). It takes the place of ssh_tailscale, which knew only the
-- first two.
ALTER TABLE servers ADD COLUMN ssh TEXT NOT NULL DEFAULT 'everyone';
UPDATE servers SET ssh = 'tailscale' WHERE ssh_tailscale = 1;
ALTER TABLE servers DROP COLUMN ssh_tailscale;

-- +goose Down
ALTER TABLE servers ADD COLUMN ssh_tailscale INTEGER NOT NULL DEFAULT 0;
UPDATE servers SET ssh_tailscale = 1 WHERE ssh = 'tailscale';
ALTER TABLE servers DROP COLUMN ssh;
