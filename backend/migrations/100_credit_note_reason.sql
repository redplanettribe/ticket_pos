-- A Credit Note's reason is a reason, not a reversal route (#481, parent
-- #478, ADR 0061).
--
-- Migration 098 named the column reversal_reason and admitted exactly the
-- five Sale Reversal routes, because a Sale Reversal was the only act that
-- owed a Credit Note. A Sale Invoice Reissue owes one with no reversal
-- behind it: the Sale stands, paid, and the document is credited only so a
-- corrected factura can follow. The column is therefore renamed to what it
-- is — the Credit Note's reason — and admits `reissue` beside the five
-- routes. Every existing row is a reversal's and keeps its value; the
-- five routes mean exactly what they meant.
--
-- The kind_credit CHECK is unchanged in meaning: a Credit Note, and only a
-- Credit Note, names what it credits and why. Nothing here ties a reason to
-- a reversal — no reversal id is stored — so "a Credit Note has a reason"
-- was already all it said; it is rewritten only because the column it
-- names is renamed. A reissue Credit Note has no reversal, and the schema
-- has never asked for one.
ALTER TABLE invoicing_invoices
    RENAME COLUMN reversal_reason TO credit_note_reason;

ALTER TABLE invoicing_invoices
    DROP CONSTRAINT invoicing_invoices_reversal_reason_check,
    ADD CONSTRAINT invoicing_invoices_credit_note_reason_check
        CHECK (credit_note_reason IN ('customer', 'platform', 'import_undo', 'staff_reversal', 'correction', 'reissue')),
    DROP CONSTRAINT invoicing_invoices_kind_credit,
    ADD CONSTRAINT invoicing_invoices_kind_credit
        CHECK ((kind = 'credit_note') = (credits_invoice_id IS NOT NULL)
           AND (kind = 'credit_note') = (credit_note_reason IS NOT NULL));
