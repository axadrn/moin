-- +goose Up
-- ============================================================================
-- GITHUB
-- The moin GitHub App. A user installs it on accounts and organizations;
-- moin stores each installation with the account it belongs to. An app can
-- follow one repo and branch: deploys of a ref download that commit from
-- GitHub, and with auto_deploy a push to the branch deploys it. Deploys
-- remember the ref and commit they came from, for the commit status.
-- ============================================================================
CREATE TABLE IF NOT EXISTS github_installations (
    installation_id INTEGER PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    account TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_github_installations_workspace ON github_installations(workspace_id);

ALTER TABLE apps ADD COLUMN repo TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN branch TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN auto_deploy INTEGER NOT NULL DEFAULT 0;
ALTER TABLE apps ADD COLUMN installation_id INTEGER NOT NULL DEFAULT 0;

ALTER TABLE deploys ADD COLUMN ref TEXT NOT NULL DEFAULT '';
ALTER TABLE deploys ADD COLUMN commit_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE deploys ADD COLUMN commit_message TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deploys DROP COLUMN commit_message;
ALTER TABLE deploys DROP COLUMN commit_sha;
ALTER TABLE deploys DROP COLUMN ref;
ALTER TABLE apps DROP COLUMN installation_id;
ALTER TABLE apps DROP COLUMN auto_deploy;
ALTER TABLE apps DROP COLUMN branch;
ALTER TABLE apps DROP COLUMN repo;
DROP INDEX IF EXISTS idx_github_installations_workspace;
DROP TABLE IF EXISTS github_installations;
