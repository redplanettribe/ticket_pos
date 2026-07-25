-- Payments: a Customer's attempt to pay for tickets through a Payment Provider
-- during an Online Sale (ADR 0012). A Payment begins 'pending' when checkout
-- starts and ends 'approved' (the moment its Ticket Sale is recorded), 'failed'
-- (declined or cancelled), or 'expired' (abandoned). A Ticket Sale exists only
-- for an approved Payment; a Payment that never completes never becomes a sale.

CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- Which Payment Provider implementation handled this attempt ('stub' in
    -- development, 'payphone' in production).
    provider TEXT NOT NULL,
    -- Our own id for the attempt, generated at begin-checkout and carried through
    -- the provider's redirect legs. Confirm is keyed on it, so it is unique.
    client_transaction_id TEXT NOT NULL UNIQUE,
    -- The provider's id for the same attempt, for cross-referencing the provider
    -- dashboard during support. Null until the provider has assigned one.
    provider_transaction_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'failed', 'expired')),
    amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0),
    -- The checkout identity as entered, recorded verbatim. The Customer row is
    -- created only when the Payment is approved and its Ticket Sale committed.
    customer_email TEXT NOT NULL,
    customer_first_name TEXT NOT NULL,
    customer_last_name TEXT NOT NULL,
    -- The Ticket Sale this Payment produced. Null while pending/failed/expired —
    -- and, in the one loudly-logged incident case, on an approved Payment whose
    -- sale commit failed after the provider took the money.
    ticket_sale_id UUID REFERENCES ticket_sales (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX payments_event_id_idx ON payments (event_id);
-- Serves the derived Capacity Hold query (ADR 0013): pending Payments younger
-- than the hold window, per event.
CREATE INDEX payments_status_created_at_idx ON payments (status, created_at);

-- Payment lines: the snapshot of what the Customer is paying for — Ticket Type,
-- quantity, and the unit price at the moment checkout began. The approved sale's
-- Ticket Sale Lines are written from this snapshot, so a catalog price edit
-- mid-payment cannot change what was bought.
CREATE TABLE payment_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id UUID NOT NULL REFERENCES payments (id) ON DELETE CASCADE,
    ticket_type_id UUID NOT NULL REFERENCES ticket_types (id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price_cents INTEGER NOT NULL CHECK (unit_price_cents >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX payment_lines_payment_id_idx ON payment_lines (payment_id);
CREATE INDEX payment_lines_ticket_type_id_idx ON payment_lines (ticket_type_id);

-- Rework the ticket_sales Payment Method rules for Online Sales: the value set
-- gains 'payphone' (the Payment Provider that collected the money), and a
-- Payment Method is now required on channel = 'online' sales as well as on
-- source = 'direct' sales — and still forbidden everywhere else.
ALTER TABLE ticket_sales DROP CONSTRAINT ticket_sales_payment_method_check;
ALTER TABLE ticket_sales DROP CONSTRAINT ticket_sales_payment_method_ck;
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_payment_method_check
    CHECK (payment_method IN ('cash', 'transfer', 'payphone'));
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_payment_method_ck CHECK (
    ((channel = 'online' OR source = 'direct') AND payment_method IS NOT NULL)
    OR (channel <> 'online' AND source IS DISTINCT FROM 'direct' AND payment_method IS NULL)
);
