-- When a Ticket Sale was reversed, and who asked for it (#117, ADR 0018).
--
-- A reversed Ticket Sale carried nothing but its `reversed` status. That was
-- survivable while a Sale Import undo was the only reversal path, because the
-- `sale_import_batches` row held the when and the who. Customer-initiated Sale
-- Reversal has no batch to hang anything off, so the provenance has to live on
-- the Ticket Sale itself.
--
-- Two columns, not a `sale_reversals` audit table: ADR 0018 weighed the table
-- and deferred it as disproportionate until the feature has volume — these two
-- answer the questions that actually get asked ("when did this go?" and "did
-- the buyer do it or did we?").

ALTER TABLE ticket_sales ADD COLUMN reversed_at TIMESTAMPTZ;

-- The KIND of actor behind the Sale Reversal, not their identity. The glossary
-- names exactly two ways a Ticket Sale can be voided: the Customer reversing
-- their own Online Sale within the Reversal Window, and staff undoing a Sale
-- Import. Both values ship now — 'staff' is written by the import undo landing
-- with this migration, and 'customer' is written by the Storefront reversal
-- that follows — so the customer path needs no further schema change.
--
-- Deliberately not a member_id/customer_id foreign key. A Sale Import undo's
-- acting Member is already recorded on the batch (created_by_member_id), and a
-- reversing Customer is by definition the sale's own buyer, already snapshotted
-- on this row. What neither answers is which SIDE the reversal came from, and
-- that is the whole question this column exists for.
--
-- An extensible set, expressed like payment_method (migration 030): a value
-- added later — say a Platform Operator acting on an incident — is a one-line
-- constraint change, not a redesign.
ALTER TABLE ticket_sales ADD COLUMN reversed_by TEXT
    CHECK (reversed_by IN ('customer', 'staff'));

-- The pair is written and read together: a time with no actor cannot say who,
-- an actor with no time cannot say when. Both set or both null.
--
-- Note what this does NOT say: it does not require a reversed sale to carry
-- them. Every row already in `reversed` status the day this ships was reversed
-- before anything recorded when, and it stays null — a fabricated timestamp
-- would read as fact and be wrong. The same choice the Tax ID snapshot made
-- (migration 026): history is shown as it is, never backfilled.
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_reversal_pair_ck CHECK (
    (reversed_at IS NULL) = (reversed_by IS NULL)
);

-- An active sale has no reversal to describe. This direction IS enforceable
-- against existing rows (no active sale carries provenance, since the columns
-- were only just added) and it keeps the null-on-an-active-row promise the
-- Sales list makes to staff.
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_reversal_status_ck CHECK (
    status = 'reversed' OR reversed_at IS NULL
);
