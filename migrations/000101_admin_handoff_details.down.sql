ALTER TABLE admin_handoffs
    DROP COLUMN IF EXISTS identifiers,
    DROP COLUMN IF EXISTS service,
    DROP COLUMN IF EXISTS priority;
