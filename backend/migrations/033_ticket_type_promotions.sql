-- The Promotion: a time-boxed override of a Ticket Type's List Price
-- (#138, ADR 0021).
--
-- Not a coupon code and not a percentage: an absolute Promotional Price in the
-- Organization's currency, held for a scheduled window. At most one per Ticket
-- Type — the "one slot" the ADR chose over a queue of phases, and the reason
-- this is a table of its own rather than three nullable columns on
-- ticket_types: absent is the common case, and a Promotion's window and price
-- only mean anything together.
CREATE TABLE ticket_type_promotions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- UNIQUE is the one slot, enforced by the database rather than by a
    -- read-then-write in the service. Removing the Promotion frees the slot;
    -- deleting the Ticket Type takes its Promotion with it.
    ticket_type_id UUID NOT NULL UNIQUE REFERENCES ticket_types (id) ON DELETE CASCADE,

    -- Zero is allowed — a Promotion may make a Ticket Type free for the window,
    -- and the zero-total checkout path already settles without a Payment
    -- Provider (ADR 0017). Strictly-below-the-List-Price is the invariant the
    -- service enforces on both writes; it spans two tables, so it is not a
    -- CHECK here.
    promotional_price_cents INTEGER NOT NULL CHECK (promotional_price_cents >= 0),

    -- NULL start means "live from the moment it was saved": the common case is
    -- an organizer discounting today, and making them type now's timestamp to
    -- say so would be ceremony. The end is required — a Promotion that never
    -- ends is a List Price change, which the Ticket Type already offers.
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A window that ends before it opens is never live. Cheap to state here and
    -- true of every row regardless of who wrote it.
    CONSTRAINT ticket_type_promotions_window_ck CHECK (starts_at IS NULL OR starts_at < ends_at)
);
