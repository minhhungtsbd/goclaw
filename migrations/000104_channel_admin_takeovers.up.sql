CREATE TABLE channel_admin_takeovers (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_name VARCHAR(100) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    agent_key VARCHAR(100) NOT NULL,
    admin_message_id VARCHAR(255) NOT NULL DEFAULT '',
    last_admin_message TEXT NOT NULL DEFAULT '',
    taken_over_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    released_at TIMESTAMPTZ,
    released_by VARCHAR(255) NOT NULL DEFAULT '',
    release_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT channel_admin_takeovers_expiry_check CHECK (expires_at > taken_over_at),
    CONSTRAINT channel_admin_takeovers_scope_unique UNIQUE (tenant_id, channel_name, chat_id)
);

CREATE INDEX idx_channel_admin_takeovers_active
    ON channel_admin_takeovers (tenant_id, channel_name, expires_at DESC)
    WHERE released_at IS NULL;
