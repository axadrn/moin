-- +goose Up
-- Whether the control plane updates the agents of a workspace to its own
-- version by itself. On by default.
ALTER TABLE workspaces ADD COLUMN auto_update_agents INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE workspaces DROP COLUMN auto_update_agents;
