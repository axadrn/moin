-- The copies a server sends to moin cloud. Nothing used the table of 001
-- yet, so it is made again, with the workspace and the server's name: a
-- copy outlives its server, and a disconnect never hides one.
DROP TABLE copies;
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

-- When the subscription ended: 30 days later the copies go.
ALTER TABLE workspaces ADD COLUMN ended INTEGER;
-- When the last mail about a full storage went out: one a day at most.
ALTER TABLE workspaces ADD COLUMN storage_mail INTEGER NOT NULL DEFAULT 0;
