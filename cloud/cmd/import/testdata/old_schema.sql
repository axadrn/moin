-- The tables of the old hub that the import reads, as its migrations 001 to
-- 064 left them (commit 27a885a7, hub/internal/db/migrations). Only for the
-- test: it builds an old database with fake rows from this.

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    -- Explicitly nullable for passwordless authentication.
    pending_email TEXT NULL,
    email_verified_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    name TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP
, email_alerts INTEGER NOT NULL DEFAULT 1, github_id INTEGER, github_login TEXT NOT NULL DEFAULT '', recovery_key_hash TEXT, recovery_key_expires_at TIMESTAMP, github_disconnected_at TIMESTAMP);

CREATE UNIQUE INDEX idx_users_github_id ON users(github_id) WHERE github_id IS NOT NULL;

CREATE UNIQUE INDEX idx_users_recovery_key_hash ON users(recovery_key_hash) WHERE recovery_key_hash IS NOT NULL;

CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    created_by TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, auto_update_daemons INTEGER NOT NULL DEFAULT 1, business_confirmed_by TEXT, business_confirmed_at TIMESTAMP,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_workspaces_slug ON workspaces(slug);

CREATE TABLE offers (
    email TEXT PRIMARY KEY,
    trial_days INTEGER NOT NULL,
    coupon_id TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMP,
    claimed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
, claimed_user_id TEXT, claimed_workspace_id TEXT);

CREATE TABLE subscriptions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT UNIQUE NOT NULL,
    plan_id TEXT NOT NULL DEFAULT 'free',
    status TEXT NOT NULL DEFAULT 'active',
    provider_customer_id TEXT,
    provider_subscription_id TEXT,
    provider_subscription_created_at TIMESTAMP,
    provider_state_updated_at TIMESTAMP,
    provider_payment_updated_at TIMESTAMP,
    checkout_attempt_id TEXT,
    checkout_provider TEXT NOT NULL DEFAULT '',
    checkout_plan_id TEXT NOT NULL DEFAULT '',
    checkout_interval TEXT NOT NULL DEFAULT '',
    checkout_url TEXT,
    checkout_expires_at TIMESTAMP,
    checkout_pending_until TIMESTAMP,
    current_period_end TIMESTAMP,
    amount INTEGER,
    currency TEXT NOT NULL DEFAULT '',
    interval TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    provider TEXT NOT NULL DEFAULT '', coupon_id TEXT,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE TABLE workspace_members (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE (workspace_id, user_id)
);

CREATE INDEX idx_subscriptions_provider ON subscriptions(provider);

CREATE INDEX idx_subscriptions_status ON subscriptions(status);

CREATE INDEX idx_subscriptions_workspace_id ON subscriptions(workspace_id);

CREATE INDEX idx_workspace_members_user_id ON workspace_members(user_id);

CREATE INDEX idx_workspace_members_workspace_id ON workspace_members(workspace_id);

CREATE INDEX idx_workspace_members_workspace_role ON workspace_members(workspace_id, role);

