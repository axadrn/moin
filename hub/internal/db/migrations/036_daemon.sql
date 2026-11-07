-- +goose Up
-- One word per thing: the moin process on a server is the daemon, no
-- longer the agent, in the schema and the activity too.
ALTER TABLE servers RENAME COLUMN agent_token_hash TO daemon_token_hash;
ALTER TABLE servers RENAME COLUMN agent_version TO daemon_version;
ALTER TABLE workspaces RENAME COLUMN auto_update_agents TO auto_update_daemons;

UPDATE activity_events SET action = 'server.daemon_updated' WHERE action = 'server.agent_updated';
UPDATE activity_events SET action = 'server.daemon_update_failed' WHERE action = 'server.agent_update_failed';
UPDATE activity_events SET action = 'workspace.daemon_updates' WHERE action = 'workspace.agent_updates';

-- +goose Down
UPDATE activity_events SET action = 'workspace.agent_updates' WHERE action = 'workspace.daemon_updates';
UPDATE activity_events SET action = 'server.agent_update_failed' WHERE action = 'server.daemon_update_failed';
UPDATE activity_events SET action = 'server.agent_updated' WHERE action = 'server.daemon_updated';

ALTER TABLE workspaces RENAME COLUMN auto_update_daemons TO auto_update_agents;
ALTER TABLE servers RENAME COLUMN daemon_version TO agent_version;
ALTER TABLE servers RENAME COLUMN daemon_token_hash TO agent_token_hash;
