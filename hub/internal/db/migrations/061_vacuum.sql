-- +goose NO TRANSACTION
-- +goose Up
-- 051 deleted the sealed secrets and dropped their columns, but SQLite
-- only marks such pages free: the old values stayed in the file. VACUUM
-- writes the database anew, so they are gone from it, and the checkpoint
-- empties the write-ahead log, which may still hold them. VACUUM cannot
-- run in a transaction, so this migration runs without one.
VACUUM;
PRAGMA wal_checkpoint(TRUNCATE);

-- +goose Down
-- Nothing to give back.
