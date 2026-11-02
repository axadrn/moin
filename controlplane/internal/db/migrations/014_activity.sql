-- +goose Up
-- ============================================================================
-- ACTIVITY
-- Each event says how it came in (web, cli, tui, api, mcp, github, hook or
-- system), the API token by name, the actor's email at the time, so it stays
-- when the account is deleted, the client IP and user agent of the request,
-- and the app and server it touched, so the activity can be filtered by them.
-- Older rows get them from their metadata, their user and their app.
-- ============================================================================
ALTER TABLE workspace_audit_events ADD COLUMN via TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_audit_events ADD COLUMN token_name TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_audit_events ADD COLUMN actor_email TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_audit_events ADD COLUMN ip TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_audit_events ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_audit_events ADD COLUMN app TEXT NOT NULL DEFAULT '';
ALTER TABLE workspace_audit_events ADD COLUMN server TEXT NOT NULL DEFAULT '';

UPDATE workspace_audit_events
SET via = COALESCE(json_extract(metadata, '$.via'), '')
WHERE json_valid(metadata);

UPDATE workspace_audit_events
SET actor_email = COALESCE((SELECT email FROM users WHERE users.id = workspace_audit_events.actor_user_id), '');

UPDATE workspace_audit_events
SET app = COALESCE((
    SELECT name FROM apps
    WHERE apps.workspace_id = workspace_audit_events.workspace_id AND apps.id = workspace_audit_events.target_id
), target_id)
WHERE target_type = 'app';

UPDATE workspace_audit_events SET server = target_id WHERE target_type = 'server';

UPDATE workspace_audit_events
SET server = COALESCE((
    SELECT servers.name FROM apps JOIN servers ON servers.id = apps.server_id
    WHERE apps.workspace_id = workspace_audit_events.workspace_id AND apps.name = workspace_audit_events.app
), CASE WHEN json_valid(metadata) THEN json_extract(metadata, '$.server') END, '')
WHERE target_type = 'app';

CREATE INDEX IF NOT EXISTS idx_workspace_audit_events_app ON workspace_audit_events(workspace_id, app, created_at);
CREATE INDEX IF NOT EXISTS idx_workspace_audit_events_server ON workspace_audit_events(workspace_id, server, created_at);
CREATE INDEX IF NOT EXISTS idx_workspace_audit_events_actor ON workspace_audit_events(workspace_id, actor_user_id, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_workspace_audit_events_actor;
DROP INDEX IF EXISTS idx_workspace_audit_events_server;
DROP INDEX IF EXISTS idx_workspace_audit_events_app;
ALTER TABLE workspace_audit_events DROP COLUMN server;
ALTER TABLE workspace_audit_events DROP COLUMN app;
ALTER TABLE workspace_audit_events DROP COLUMN user_agent;
ALTER TABLE workspace_audit_events DROP COLUMN ip;
ALTER TABLE workspace_audit_events DROP COLUMN actor_email;
ALTER TABLE workspace_audit_events DROP COLUMN token_name;
ALTER TABLE workspace_audit_events DROP COLUMN via;
