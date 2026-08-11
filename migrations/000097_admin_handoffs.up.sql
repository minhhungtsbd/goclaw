CREATE TABLE admin_handoffs (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    admin_channel TEXT NOT NULL,
    admin_chat_id TEXT NOT NULL,
    source_channel TEXT NOT NULL,
    source_chat_id TEXT NOT NULL,
    summary TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'delivery_failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    completion_message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_admin_handoffs_pending ON admin_handoffs (tenant_id, admin_channel, admin_chat_id, created_at DESC) WHERE status = 'pending';
