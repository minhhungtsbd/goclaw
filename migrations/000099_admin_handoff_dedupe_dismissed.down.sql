DROP INDEX IF EXISTS idx_admin_handoffs_pending_dedupe;

ALTER TABLE admin_handoffs
    DROP CONSTRAINT IF EXISTS admin_handoffs_status_check;

ALTER TABLE admin_handoffs
    ADD CONSTRAINT admin_handoffs_status_check
    CHECK (status IN ('pending', 'completed', 'delivery_failed'));

ALTER TABLE admin_handoffs
    DROP COLUMN IF EXISTS dedupe_key;
