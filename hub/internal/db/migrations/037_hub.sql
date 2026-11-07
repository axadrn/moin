-- +goose Up
-- One word per thing: the moin web app and API is the hub, no longer the
-- control plane, in the activity too.
UPDATE activity_events
SET action = 'hub.' || substr(action, length('controlplane.') + 1), target_type = 'hub'
WHERE action LIKE 'controlplane.%';

-- +goose Down
UPDATE activity_events
SET action = 'controlplane.' || substr(action, length('hub.') + 1), target_type = 'controlplane'
WHERE action LIKE 'hub.%';
