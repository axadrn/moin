-- +goose Up
-- The trial starts when the workspace's first server connects. A trial
-- without an end waits for that server. A workspace that never had a
-- server, pays nothing and has no offer gets its trial back, waiting, also
-- when its old trial ran out. A trial of a workspace with a server stays
-- as it is.
UPDATE subscriptions SET current_period_end = NULL, updated_at = CURRENT_TIMESTAMP
WHERE plan_id = 'pro' AND status = 'trialing'
  AND provider_subscription_id IS NULL AND coupon_id IS NULL
  AND workspace_id NOT IN (SELECT workspace_id FROM servers)
  AND workspace_id NOT IN (SELECT workspace_id FROM activity_events WHERE action = 'server.installed');

-- +goose Down
-- The old ends are gone: a waiting trial stays waiting.
