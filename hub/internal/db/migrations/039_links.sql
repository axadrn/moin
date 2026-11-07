-- +goose Up
-- ============================================================================
-- LINKS AND NAMES
-- A link names a thing <name>-<id>, the ID being its UUID without dashes,
-- and only the ID counts. The buckets 023 made from the old backup settings
-- got an ID without dashes; they get them like every other ID.
--
-- Apps can be renamed. An app's folder is the name it was created with and
-- never changes: its server keeps the app under it (its folder there, its
-- backups' folder in the bucket, its instant address), so a rename loses
-- nothing. Two apps of a workspace never share one.
-- ============================================================================
UPDATE apps SET bucket_id = substr(bucket_id, 1, 8) || '-' || substr(bucket_id, 9, 4) || '-' || substr(bucket_id, 13, 4) || '-' || substr(bucket_id, 17, 4) || '-' || substr(bucket_id, 21, 12)
WHERE length(bucket_id) = 32;
UPDATE app_buckets SET bucket_id = substr(bucket_id, 1, 8) || '-' || substr(bucket_id, 9, 4) || '-' || substr(bucket_id, 13, 4) || '-' || substr(bucket_id, 17, 4) || '-' || substr(bucket_id, 21, 12)
WHERE length(bucket_id) = 32;
UPDATE buckets SET id = substr(id, 1, 8) || '-' || substr(id, 9, 4) || '-' || substr(id, 13, 4) || '-' || substr(id, 17, 4) || '-' || substr(id, 21, 12)
WHERE length(id) = 32;

ALTER TABLE apps ADD COLUMN folder TEXT NOT NULL DEFAULT '';
UPDATE apps SET folder = name;
CREATE UNIQUE INDEX IF NOT EXISTS idx_apps_folder ON apps(workspace_id, folder);

-- +goose Down
DROP INDEX IF EXISTS idx_apps_folder;
ALTER TABLE apps DROP COLUMN folder;
