-- +goose Up
-- An offer remembers who got it: the account and its workspace. An admin may
-- give it to an account with another address than the one it waited for.
-- No foreign keys: SQLite drops no column that has one, and a deleted account
-- leaves only an id nobody matches.
ALTER TABLE offers ADD COLUMN claimed_user_id TEXT;
ALTER TABLE offers ADD COLUMN claimed_workspace_id TEXT;
-- Offers claimed before: the account with that address got it, on the first
-- workspace it created.
UPDATE offers SET claimed_user_id = (
    SELECT users.id FROM users WHERE lower(trim(users.email)) = offers.email
) WHERE claimed_at IS NOT NULL;
UPDATE offers SET claimed_workspace_id = (
    SELECT workspaces.id FROM workspaces WHERE workspaces.created_by = offers.claimed_user_id
    ORDER BY workspaces.created_at, workspaces.id LIMIT 1
) WHERE claimed_user_id IS NOT NULL;

-- A note on a user, only for admins. It goes with the account.
CREATE TABLE user_notes (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    note TEXT NOT NULL,
    updated_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE user_notes;
ALTER TABLE offers DROP COLUMN claimed_workspace_id;
ALTER TABLE offers DROP COLUMN claimed_user_id;
