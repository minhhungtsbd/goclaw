ALTER TABLE admin_handoffs
    ADD COLUMN ticket_number BIGINT;

CREATE SEQUENCE admin_handoff_ticket_number_seq;

UPDATE admin_handoffs
SET ticket_number = nextval('admin_handoff_ticket_number_seq');

ALTER TABLE admin_handoffs
    ALTER COLUMN ticket_number SET DEFAULT nextval('admin_handoff_ticket_number_seq'),
    ALTER COLUMN ticket_number SET NOT NULL,
    ADD CONSTRAINT admin_handoffs_ticket_number_key UNIQUE (ticket_number);

ALTER SEQUENCE admin_handoff_ticket_number_seq
    OWNED BY admin_handoffs.ticket_number;
