-- +goose Up
-- Apps are pinned instead of put in order by hand. Lists sort themselves:
-- what needs you, then what deploys, then pinned apps, then by name. The
-- hand order and the choice of list or grid go.
DROP INDEX IF EXISTS idx_apps_workspace_position;
DROP INDEX IF EXISTS idx_servers_workspace_position;
ALTER TABLE apps DROP COLUMN position;
ALTER TABLE servers DROP COLUMN position;
ALTER TABLE users DROP COLUMN apps_view;
ALTER TABLE users DROP COLUMN servers_view;
ALTER TABLE apps ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
-- How each member sorts the apps: smart, recent or server.
ALTER TABLE users ADD COLUMN app_sort TEXT NOT NULL DEFAULT 'smart';

-- +goose Down
ALTER TABLE users DROP COLUMN app_sort;
ALTER TABLE apps DROP COLUMN pinned;
ALTER TABLE users ADD COLUMN servers_view TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN apps_view TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE apps ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_apps_workspace_position ON apps(workspace_id, position);
CREATE INDEX IF NOT EXISTS idx_servers_workspace_position ON servers(workspace_id, position);
