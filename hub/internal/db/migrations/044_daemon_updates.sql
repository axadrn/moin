-- +goose Up
-- A daemon update that is not done yet: the note the hub keeps while it runs
-- the steps of daemon_steps.go. If the hub or the server stops halfway, the
-- next connect of the daemon finishes it. stopped_apps are the IDs of the
-- apps the update stopped, as a JSON array, so it starts exactly those again.
CREATE TABLE daemon_updates (
    server_id TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    from_version TEXT NOT NULL,
    stopped_apps TEXT NOT NULL DEFAULT '[]',
    started_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE daemon_updates;
