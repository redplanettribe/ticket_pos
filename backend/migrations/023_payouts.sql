-- Payouts: the recorded settlements in which the platform transfers accumulated
-- Net Proceeds to an Organization (ADR 0014). A Payout is a bare fact — amount,
-- the day it was paid, an optional note — with no workflow, no states and no
-- approvals: the platform operator records one directly in the database after
-- settling off-platform, and the Organization only ever reads it.
--
-- Money is settled with the Organization, never with an individual Event, so
-- there is deliberately no event_id here. Subtracting the sum of these rows from
-- the Organization's Net Proceeds gives its Withdrawable Balance, which may be
-- negative when a sale is reversed after it has already been paid out.

CREATE TABLE payouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    -- The day the money left the platform, in the operator's reckoning: a date,
    -- not an instant, because that is what a bank transfer is reconciled by.
    paid_at DATE NOT NULL,
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The payout history reads newest first, per Organization.
CREATE INDEX payouts_organization_paid_at_idx ON payouts (organization_id, paid_at DESC, id DESC);
