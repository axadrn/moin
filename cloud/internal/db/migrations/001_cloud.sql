-- moin cloud. Times are Unix seconds. Tokens are stored only as their
-- SHA-256, in hex.

-- A workspace is one customer. Its mail is the account: whoever signs in
-- with it, by GitHub or a mail link, is in.
CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    mail TEXT NOT NULL UNIQUE,
    -- trial, paid or ended.
    status TEXT NOT NULL DEFAULT 'trial' CHECK (status IN ('trial', 'paid', 'ended')),
    -- How many days the trial lasts. An early backer's offer keeps its
    -- free year.
    trial_days INTEGER NOT NULL DEFAULT 30,
    -- Empty until the first report: the trial starts then.
    trial_ends INTEGER,
    -- The last trial mail sent: 0 none, 25 five days before the end, 30
    -- at the end.
    trial_mails INTEGER NOT NULL DEFAULT 0,
    stripe_customer TEXT,
    stripe_subscription TEXT,
    paid_until INTEGER,
    coupon TEXT,
    -- When the subscription ended: 30 days later the copies go.
    ended INTEGER,
    -- When the last mail about a full storage went out: one a day at most.
    storage_mail INTEGER NOT NULL DEFAULT 0,
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
-- `moin watcher connect` that waits for a browser to allow its code; mail is
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
    -- What its last report said, for the counts: when it came, how many
    -- apps it had, each app's release as JSON, and the knocks in all.
    last_report INTEGER,
    apps INTEGER NOT NULL DEFAULT 0,
    releases TEXT NOT NULL DEFAULT '{}',
    knocks INTEGER,
    UNIQUE (workspace_id, name)
);

-- A copy a server sent to moin cloud. It has the workspace and the
-- server's name, no foreign key: a copy outlives its server, and a
-- disconnect never hides one.
CREATE TABLE copies (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    -- The name of the server that sent it.
    server TEXT NOT NULL,
    app TEXT NOT NULL,
    database TEXT NOT NULL,
    -- When the server took the copy.
    made_at INTEGER NOT NULL,
    -- The bytes the server said it sends, and once it is done the bytes
    -- that are in the bucket.
    size INTEGER NOT NULL,
    -- uploading: moin cloud gave out the address to upload to. done: the
    -- server said it is done, and the copy is in the bucket.
    state TEXT NOT NULL CHECK (state IN ('uploading', 'done')),
    -- When moin cloud gave out the address. The bucket's lock counts from
    -- about then.
    created INTEGER NOT NULL
);
CREATE INDEX copies_database ON copies(workspace_id, server, app, database, made_at);

-- Sums over everyone, by day (UTC), for the landing page: deploys and
-- knocks. Nothing in it names a customer.
CREATE TABLE counts (
    day TEXT NOT NULL,
    name TEXT NOT NULL,
    n INTEGER NOT NULL,
    PRIMARY KEY (day, name)
);
