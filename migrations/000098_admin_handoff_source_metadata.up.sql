ALTER TABLE admin_handoffs
    ADD COLUMN source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
