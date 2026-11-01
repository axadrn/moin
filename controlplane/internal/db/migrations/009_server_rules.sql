-- +goose Up
-- ============================================================================
-- SERVER RULES
-- What the owner of a server allows ops to do there, from /etc/ops/rules on
-- the server, as the agent last reported it: JSON, empty without rules. The
-- control plane only shows them; the agent enforces them.
-- ============================================================================
ALTER TABLE servers ADD COLUMN rules TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN rules;
