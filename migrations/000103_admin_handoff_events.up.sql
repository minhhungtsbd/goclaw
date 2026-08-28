CREATE INDEX idx_admin_handoffs_tenant_status_created
    ON admin_handoffs (tenant_id, status, created_at DESC);

CREATE TABLE admin_handoff_events (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    handoff_id UUID NOT NULL REFERENCES admin_handoffs(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_handoff_events_handoff_created
    ON admin_handoff_events (tenant_id, handoff_id, created_at);
