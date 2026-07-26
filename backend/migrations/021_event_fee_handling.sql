-- Fee Handling: the per-Event choice of whether the buyer price is raised to
-- cover the Platform Fee and its Fee IVA (ADR 0014). It never changes who is
-- charged — the fee is always withheld from the Organization — only whether the
-- Customer's price carries it. 'pass_on' is the default, so an Organization
-- nets exactly the price it set unless it decides otherwise; existing Events
-- take that default too.
--
-- A change here is effective for future checkouts only: every Payment snapshots
-- its fee amounts at begin-checkout, so a flip mid-payment cannot rewrite what
-- a Customer is already paying.
ALTER TABLE events
    ADD COLUMN fee_handling TEXT NOT NULL DEFAULT 'pass_on'
        CHECK (fee_handling IN ('pass_on', 'absorb'));
