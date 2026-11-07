-- +goose Up
-- The order of apps and servers, set by hand per workspace: lists show them
-- by position. New ones go to the end. The order so far was by name.
ALTER TABLE apps ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
UPDATE apps SET position = (SELECT COUNT(*) FROM apps AS other WHERE other.workspace_id = apps.workspace_id AND (other.name < apps.name OR (other.name = apps.name AND other.id < apps.id)));
UPDATE servers SET position = (SELECT COUNT(*) FROM servers AS other WHERE other.workspace_id = servers.workspace_id AND (other.name < servers.name OR (other.name = servers.name AND other.id < servers.id)));
CREATE INDEX IF NOT EXISTS idx_apps_workspace_position ON apps(workspace_id, position);
CREATE INDEX IF NOT EXISTS idx_servers_workspace_position ON servers(workspace_id, position);

-- How each member sees the Apps and Servers pages: a list or a grid. Empty
-- is the default: apps as a list, servers as a grid.
ALTER TABLE users ADD COLUMN apps_view TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN servers_view TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN servers_view;
ALTER TABLE users DROP COLUMN apps_view;
DROP INDEX IF EXISTS idx_servers_workspace_position;
DROP INDEX IF EXISTS idx_apps_workspace_position;
ALTER TABLE servers DROP COLUMN position;
ALTER TABLE apps DROP COLUMN position;
