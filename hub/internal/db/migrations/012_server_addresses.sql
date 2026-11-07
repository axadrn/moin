-- +goose Up
-- ============================================================================
-- SERVER ADDRESSES
-- The public IPv4 and IPv6 the agent reports of its own server, space
-- separated. With servers.address, the one it connected from, the DNS check
-- knows both and accepts a domain that resolves to either.
-- ============================================================================
ALTER TABLE servers ADD COLUMN addresses TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN addresses;
