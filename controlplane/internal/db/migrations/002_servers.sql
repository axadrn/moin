-- +goose Up
-- ============================================================================
-- SERVERS
-- A machine with a moin agent. The install token connects the agent once and
-- is cleared then; the agent token authorizes every session after that. Only
-- SHA-256 hashes of both are stored.
-- ============================================================================
CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    install_token_hash TEXT UNIQUE,
    install_expires_at TIMESTAMP,
    agent_token_hash TEXT UNIQUE,
    hostname TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    arch TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    last_seen_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    UNIQUE (workspace_id, name)
);

CREATE INDEX IF NOT EXISTS idx_servers_workspace_id ON servers(workspace_id);

-- +goose Down
DROP INDEX IF EXISTS idx_servers_workspace_id;
DROP TABLE IF EXISTS servers;
