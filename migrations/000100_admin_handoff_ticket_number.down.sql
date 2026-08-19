ALTER TABLE admin_handoffs
    DROP CONSTRAINT IF EXISTS admin_handoffs_ticket_number_key,
    DROP COLUMN IF EXISTS ticket_number;

DROP SEQUENCE IF EXISTS admin_handoff_ticket_number_seq;
