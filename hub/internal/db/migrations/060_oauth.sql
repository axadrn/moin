-- +goose Up
-- AI agents sign in like the CLI: in the browser, as a connection. They
-- use OAuth: an MCP client registers itself (RFC 7591), the person
-- approves it, and the client trades a short code for the connection's
-- tokens. A client is only its name and where it may send the person back.
CREATE TABLE oauth_clients (
    id TEXT PRIMARY KEY,
    client_name TEXT NOT NULL,
    -- One redirect URI per line.
    redirect_uris TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP
);

-- An authorization code lives a minute and works once. It names the
-- connection the person approved; only its hash is stored, with what the
-- client must show again: the redirect URI and the PKCE challenge. It stays
-- as long as its connection, one row each, so a code shown twice still
-- ends the connection later.
CREATE TABLE oauth_codes (
    id TEXT PRIMARY KEY,
    code_hash TEXT UNIQUE NOT NULL,
    connection_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    used_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE
);

CREATE INDEX idx_oauth_codes_expires_at ON oauth_codes(expires_at);

-- +goose Down
DROP TABLE oauth_codes;
DROP TABLE oauth_clients;
