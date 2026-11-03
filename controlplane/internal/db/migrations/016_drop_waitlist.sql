-- +goose Up
-- Sign-up is open on the hosted control plane: the waitlist is gone. Who is
-- free forever is the subscription of their workspace, set under Admin.
DROP TABLE IF EXISTS waitlist;

-- +goose Down
CREATE TABLE IF NOT EXISTS waitlist (
    email TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    let_in_at TIMESTAMP,
    forever_free INTEGER NOT NULL DEFAULT 0
);
