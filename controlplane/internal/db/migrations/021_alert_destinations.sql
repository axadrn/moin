-- +goose Up
-- ============================================================================
-- ALERT DESTINATIONS
-- Where the alerts of a workspace go besides email: a Slack or Discord
-- incoming webhook, a Telegram chat, or a webhook of the team's own. target
-- is sealed with the secrets key: the webhook URL, or the Telegram bot
-- token. secret is the sealed signing secret of a webhook. hint is what the
-- lists show of the target instead. events is a comma list of the actions
-- the destination gets. failing_since and last_error say a destination
-- keeps failing; the next send that works clears them.
-- ============================================================================
CREATE TABLE IF NOT EXISTS alert_destinations (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    target TEXT NOT NULL,
    chat_id TEXT NOT NULL DEFAULT '',
    hint TEXT NOT NULL DEFAULT '',
    secret TEXT NOT NULL DEFAULT '',
    events TEXT NOT NULL,
    failing_since TIMESTAMP,
    last_error TEXT NOT NULL DEFAULT '',
    last_sent_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (workspace_id, name),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS alert_destinations;
