-- +goose Up
-- One browser login for the CLI, the TUI and AI agents. Each approval in
-- the browser is a connection: who, which client, which workspace, which
-- rights. Its access and refresh tokens hang off it; only their hashes are
-- stored. Tokens made by hand are CI tokens, with rights and maybe one app.
-- Rights are names with commas: read, deploy, env, delete, servers.
CREATE TABLE connections (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    client_name TEXT NOT NULL,
    rights TEXT NOT NULL,
    client_id TEXT NOT NULL DEFAULT '',
    resource TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP,
    ended_at TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE INDEX idx_connections_user_id ON connections(user_id);
CREATE INDEX idx_connections_workspace_id ON connections(workspace_id);

CREATE TABLE connection_tokens (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
    token_hash TEXT UNIQUE NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    used_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
);

CREATE INDEX idx_connection_tokens_connection_id ON connection_tokens(connection_id);

CREATE TABLE ci_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT UNIQUE NOT NULL,
    prefix TEXT NOT NULL,
    rights TEXT NOT NULL,
    app_id TEXT,
    last_used_at TIMESTAMP,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    FOREIGN KEY (app_id) REFERENCES apps(id) ON DELETE CASCADE
);

CREATE INDEX idx_ci_tokens_user_id ON ci_tokens(user_id);
CREATE INDEX idx_ci_tokens_workspace_id ON ci_tokens(workspace_id);

-- Personal API tokens become CI tokens: read stays read, read and write
-- becomes read, deploy and env, never delete or servers. The hash stays,
-- so the tokens keep working. The tokens of `moin login` end: it logs in
-- again with the new rights page.
INSERT INTO ci_tokens (id, user_id, workspace_id, name, token_hash, prefix, rights, last_used_at, expires_at, created_at)
SELECT id, user_id, workspace_id, name, token_hash, prefix,
       CASE scopes WHEN 'read,write' THEN 'read,deploy,env' ELSE 'read' END,
       last_used_at, expires_at, created_at
FROM api_tokens WHERE kind = 'personal';

DROP INDEX IF EXISTS idx_api_tokens_user_id;
DROP INDEX IF EXISTS idx_api_tokens_workspace_id;
DROP TABLE api_tokens;

-- A pending login of `moin login` lives ten minutes. Once approved it
-- names its connection; the next poll gets the connection's tokens.
DROP INDEX IF EXISTS idx_device_logins_expires_at;
DROP TABLE device_logins;

CREATE TABLE device_logins (
    id TEXT PRIMARY KEY,
    device_code_hash TEXT UNIQUE NOT NULL,
    user_code_hash TEXT UNIQUE NOT NULL,
    hostname TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied')),
    connection_id TEXT,
    interval_seconds INTEGER NOT NULL,
    last_polled_at TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
);

CREATE INDEX idx_device_logins_expires_at ON device_logins(expires_at);

-- +goose Down
DROP TABLE device_logins;
CREATE TABLE device_logins (
    id TEXT PRIMARY KEY,
    device_code_hash TEXT UNIQUE NOT NULL,
    user_code_hash TEXT UNIQUE NOT NULL,
    hostname TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied')),
    user_id TEXT,
    workspace_id TEXT,
    interval_seconds INTEGER NOT NULL,
    last_polled_at TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE INDEX idx_device_logins_expires_at ON device_logins(expires_at);

CREATE TABLE api_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT UNIQUE NOT NULL,
    prefix TEXT NOT NULL,
    scopes TEXT NOT NULL CHECK (scopes IN ('read', 'read,write')),
    last_used_at TIMESTAMP,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    kind TEXT NOT NULL DEFAULT 'personal' CHECK (kind IN ('personal', 'device')),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_api_tokens_user_id ON api_tokens(user_id);
CREATE INDEX idx_api_tokens_workspace_id ON api_tokens(workspace_id);
INSERT INTO api_tokens (id, user_id, workspace_id, name, token_hash, prefix, scopes, last_used_at, expires_at, created_at)
SELECT id, user_id, workspace_id, name, token_hash, prefix,
       CASE WHEN rights = 'read' THEN 'read' ELSE 'read,write' END,
       last_used_at, expires_at, created_at
FROM ci_tokens;

DROP TABLE ci_tokens;
DROP TABLE connection_tokens;
DROP TABLE connections;
