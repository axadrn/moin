-- +goose Up
-- ============================================================================
-- STOPPED APPS
-- An app its owner stopped: its containers are kept but not running. A
-- deploy, a restart or a start runs it again.
-- ============================================================================
ALTER TABLE apps ADD COLUMN stopped INTEGER NOT NULL DEFAULT 0;

-- Apply is called restart now.
UPDATE workspace_audit_events SET action = 'app.restarted' WHERE action = 'app.applied';

-- +goose Down
UPDATE workspace_audit_events SET action = 'app.applied' WHERE action = 'app.restarted';
ALTER TABLE apps DROP COLUMN stopped;
