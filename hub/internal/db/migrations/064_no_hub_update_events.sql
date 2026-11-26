-- +goose Up
-- A self-hosted hub no longer updates itself from a button, so the activity
-- of its updates is gone: update.sh on its server updates it.
DELETE FROM activity_events WHERE action IN ('hub.update_started', 'hub.updated', 'hub.rolled_back', 'hub.update_failed');

-- +goose Down
-- Nothing to give back.
