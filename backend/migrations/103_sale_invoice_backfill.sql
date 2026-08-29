-- The Sale Invoice Backfill (#508, parent #506, ADR 0064).
--
-- A Platform Operator owes a Sale Invoice to an Uninvoiced House Sale — a
-- paid Online Sale transacted before the platform invoiced House sales, or
-- before its Organization was designated House. From the moment it is owed
-- the document is an ordinary Sale Invoice: the Drainer signs, submits and
-- delivers it, a reversal credits it, a reissue supersedes it. What
-- distinguishes it is only its birth, and these two columns record that:
-- who owed it and when, so the audit trail tells a document an operator
-- declared late from one born in the checkout's own transaction.
--
-- The trail — backfilled_by, backfilled_at — is stored by email and instant
-- the way issued_by, annulled_by, reissued_by and house_designated_by are
-- (ADR 0015), and set together or not at all. Only a Sale Invoice is ever
-- backfilled: a Credit Note is owed by a reversal or a reissue, a manual
-- Tax Invoice by the operator's form. Nothing else changes: no new unique
-- index, because "one Sale Invoice per Ticket Sale" is control flow and a
-- row lock (flows doc U3), and the reissue keeps two documents per sale.
ALTER TABLE invoicing_invoices
    ADD COLUMN backfilled_by TEXT CHECK (backfilled_by IS NULL OR backfilled_by <> ''),
    ADD COLUMN backfilled_at TIMESTAMPTZ,

    -- The trail is whole or absent.
    ADD CONSTRAINT invoicing_invoices_backfill_whole
        CHECK ((backfilled_by IS NULL) = (backfilled_at IS NULL)),
    -- Only a Sale Invoice is backfilled.
    ADD CONSTRAINT invoicing_invoices_backfill_sale_only
        CHECK (backfilled_at IS NULL OR kind = 'sale');
