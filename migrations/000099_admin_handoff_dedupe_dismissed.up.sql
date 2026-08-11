ALTER TABLE admin_handoffs
    ADD COLUMN dedupe_key TEXT NOT NULL DEFAULT '';

ALTER TABLE admin_handoffs
    DROP CONSTRAINT IF EXISTS admin_handoffs_status_check;

ALTER TABLE admin_handoffs
    ADD CONSTRAINT admin_handoffs_status_check
    CHECK (status IN ('pending', 'completed', 'delivery_failed', 'dismissed'));

CREATE UNIQUE INDEX idx_admin_handoffs_pending_dedupe
    ON admin_handoffs (tenant_id, dedupe_key)
    WHERE status = 'pending' AND dedupe_key <> '';
