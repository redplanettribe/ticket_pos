-- Customer identity: a platform-global Customer record created or reused by every
-- Ticket Sale on every Sales Channel (ADR 0010, PRD #55).
--
-- A Customer is keyed on a normalised (lowercase, trimmed) email and is UNIQUE
-- across the whole platform, not per Organization: one record spans every
-- Organization the person has bought from. The profile name here is what the
-- person asserts about themselves; each Ticket Sale keeps its own immutable
-- record of what was transacted (ADR 0005 keeps that name split first/last).

CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Normalised to lowercase and trimmed by the customers service before every
    -- lookup and insert, so letter case from two box offices cannot fragment one
    -- person's history. Globally unique: a Customer is not scoped to an Organization.
    email TEXT NOT NULL UNIQUE,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    -- Null means the record exists but nobody has proven they own it. It gates
    -- sign-in and all email beyond the Sale Confirmation the sale already sends.
    -- Nothing sets this yet; only a completed one-time passcode ever will.
    verified_at TIMESTAMPTZ,
    -- Ships unused. A Ticket Sale is a financial record an Organization must be
    -- able to reconcile, so a Customer cannot simply be deleted; this column
    -- exists so soft delete is later a small change rather than a backfill.
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Every Ticket Sale references a Customer. Added nullable, backfilled from the
-- existing denormalized customer_email values, then made NOT NULL.
ALTER TABLE ticket_sales ADD COLUMN customer_id UUID REFERENCES customers (id) ON DELETE RESTRICT;

-- One Customer per distinct normalised email already present on a Ticket Sale,
-- taking the profile name from that email's most recently recorded sale. The
-- database is greenfield (migration 011 records the same), so this is expected to
-- be a no-op in practice; it is written for correctness regardless.
INSERT INTO customers (email, first_name, last_name)
SELECT DISTINCT ON (lower(btrim(customer_email)))
    lower(btrim(customer_email)),
    customer_first_name,
    customer_last_name
FROM ticket_sales
ORDER BY lower(btrim(customer_email)), created_at DESC, id DESC
ON CONFLICT (email) DO NOTHING;

UPDATE ticket_sales ts
SET customer_id = c.id
FROM customers c
WHERE c.email = lower(btrim(ts.customer_email));

ALTER TABLE ticket_sales ALTER COLUMN customer_id SET NOT NULL;

-- Supports the future "every Ticket Sale for one Customer" read (the Customer
-- Area), newest first, with an id tiebreaker matching the Sales list convention.
CREATE INDEX ticket_sales_customer_id_idx ON ticket_sales (customer_id, sold_at DESC, id DESC);
