-- Sales core: Ticket Sales, Ticket Sale Lines, and Sale Import batches.
-- Shared by all Sales Channels; this migration ships the `import` channel with
-- the `direct` Sales Source (the Organization's own off-platform sales).

CREATE TABLE sale_import_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('direct', 'external_platform')),
    created_by_member_id UUID REFERENCES members (id) ON DELETE SET NULL,
    idempotency_key TEXT NOT NULL,
    sale_count INTEGER NOT NULL DEFAULT 0 CHECK (sale_count >= 0),
    status TEXT NOT NULL DEFAULT 'committed' CHECK (status IN ('committed', 'reversed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, idempotency_key)
);

CREATE INDEX sale_import_batches_event_id_idx ON sale_import_batches (event_id);

CREATE TABLE ticket_sales (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    channel TEXT NOT NULL CHECK (channel IN ('online', 'in_person', 'import')),
    source TEXT CHECK (source IN ('direct', 'external_platform')),
    payment_method TEXT CHECK (payment_method IN ('cash', 'transfer')),
    customer_email TEXT NOT NULL,
    customer_name TEXT NOT NULL,
    sold_at TIMESTAMPTZ NOT NULL,
    confirmation_ref TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'reversed')),
    import_batch_id UUID REFERENCES sale_import_batches (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- An imported sale must carry a Sales Source; native channels must not.
    CONSTRAINT ticket_sales_import_source_ck CHECK (channel <> 'import' OR source IS NOT NULL),
    -- A Direct Sale must carry a Payment Method; only Direct Sales may.
    CONSTRAINT ticket_sales_payment_method_ck CHECK (
        (source IS DISTINCT FROM 'direct' OR payment_method IS NOT NULL)
        AND (payment_method IS NULL OR source = 'direct')
    )
);

CREATE INDEX ticket_sales_event_id_idx ON ticket_sales (event_id);
CREATE INDEX ticket_sales_import_batch_id_idx ON ticket_sales (import_batch_id);
CREATE UNIQUE INDEX ticket_sales_confirmation_ref_idx ON ticket_sales (confirmation_ref);

CREATE TABLE ticket_sale_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_sale_id UUID NOT NULL REFERENCES ticket_sales (id) ON DELETE CASCADE,
    ticket_type_id UUID NOT NULL REFERENCES ticket_types (id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    -- Unit price snapshot at sale time: the file's amount when supplied,
    -- otherwise the catalog price. 0 = comp.
    unit_price_cents INTEGER NOT NULL CHECK (unit_price_cents >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX ticket_sale_lines_sale_id_idx ON ticket_sale_lines (ticket_sale_id);
CREATE INDEX ticket_sale_lines_ticket_type_id_idx ON ticket_sale_lines (ticket_type_id);
