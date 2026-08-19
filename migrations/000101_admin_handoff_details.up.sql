ALTER TABLE admin_handoffs
    ADD COLUMN priority TEXT NOT NULL DEFAULT 'normal',
    ADD COLUMN service TEXT NOT NULL DEFAULT '',
    ADD COLUMN identifiers JSONB NOT NULL DEFAULT '[]'::jsonb;
