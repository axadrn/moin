-- moin cloud. Times are Unix seconds. Tokens are stored only as their
-- SHA-256, in hex.

-- A workspace is one customer. Its mail is the account: whoever signs in
-- with it, by GitHub or a mail link, is in.
CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    mail TEXT NOT NULL UNIQUE,
    -- trial, paid or ended.
    status TEXT NOT NULL DEFAULT 'trial' CHECK (status IN ('trial', 'paid', 'ended')),
    -- Empty until the first report: the trial starts then.
    trial_ends INTEGER,
    stripe_customer TEXT,
    stripe_subscription TEXT,
    paid_until INTEGER,
    coupon TEXT,
    -- Where alerts go besides the mail, as JSON.
    alerts TEXT NOT NULL DEFAULT '[]',
    -- What the workspace's watcher knows, as it saves it.
    watcher_state BLOB,
    created INTEGER NOT NULL
);

-- A session is a browser or a CLI that signed in.
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('browser', 'cli')),
    expires INTEGER NOT NULL
);
CREATE INDEX sessions_workspace ON sessions(workspace_id);

-- A login waits to be used. browser: a mail link to mail. cli: a
-- `moin cloud login` that waits for a browser to allow its code; mail is
-- filled in when one does.
CREATE TABLE logins (
    token_hash TEXT PRIMARY KEY,
    mail TEXT NOT NULL DEFAULT '',
    code TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK (kind IN ('browser', 'cli')),
    expires INTEGER NOT NULL,
    used INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX logins_code ON logins(code) WHERE code != '';

-- A server reports to moin cloud with its token.
CREATE TABLE servers (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);

-- A copy of a backup a server sent. It outlives its server: no foreign
-- key, so a disconnect never deletes the list of copies.
CREATE TABLE copies (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL,
    app TEXT NOT NULL,
    database TEXT NOT NULL,
    made_at INTEGER NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL
);
CREATE INDEX copies_server ON copies(server_id, app, database, made_at);
