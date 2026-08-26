-- The Sale Invoice and the Credit Note (#473, parent #471, ADR 0060).
--
-- A paid Online Sale of a House Organization's Event owes a Tax Invoice to
-- its buyer, written INSIDE the transaction that records the Ticket Sale and
-- issued afterwards by the Sale Invoice Drainer; its reversal owes a Credit
-- Note the same way. Both reuse the Tax Invoice tables of migration 096
-- additively: a document kind, the Sale it is about, what a Credit Note
-- credits and why, the one IVA rate the platform sells tickets under, when
-- the buyer was handed the document, and when the Drainer should look again.
--
-- A DOCUMENT NOW EXISTS BEFORE IT IS ISSUED. Migration 096 knew only
-- documents an operator had just signed: every row had an Issuer, an
-- environment, an emission date, a signer and signed bytes. An owed Sale
-- Invoice has none of those — there may not even be an Issuer yet, and the
-- environment is copied at SIGNING time so that an Issuer moved from test to
-- production signs still-owed documents in production. The seven issue-time
-- columns therefore become nullable TOGETHER, held to each other by one
-- CHECK: either a document is signed and has all of them, or it is not and
-- has none. The Ecuador detail row (secuencial, clave) is absent until
-- signing too, which is what "no sequence number consumed" means.
--
-- STATES. `owed` is new and comes first: recorded, not yet worked. `pending`,
-- `authorized`, `not_authorized` and `rejected` keep their meaning.
-- `needs_attention` is parked for a Platform Operator — unsignable from owed
-- (no Issuer, no certificate, expired), definitely refused or unanswered for
-- 24 h from pending. `withdrawn` was never sent and never will be: its Sale
-- reversed first, or the factura it would have credited died. `annulled` is
-- the operator's record of a manual portal act on a SIGNED document that
-- was pending or needs_attention — never on an authorized one, which is
-- credited instead — with who and when added by a later migration (099).
-- Nothing is ever deleted.
--
-- The Recipient's address was already optional on the column ('' default);
-- a Sale Invoice never has one, since the checkout asks for none.
ALTER TABLE invoicing_invoices
    -- manual: an operator typed it (every row before this migration). sale:
    -- owed by a paid House checkout. credit_note: owed by such a Sale's
    -- reversal, crediting the sale document it names.
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'manual' CHECK (kind IN ('manual', 'sale', 'credit_note')),
    -- The Ticket Sale a sale document or credit note is about; NULL on a
    -- manual document. No ON DELETE: a Ticket Sale is never deleted, and a
    -- legal artifact must outlive anything that could be.
    ADD COLUMN ticket_sale_id UUID REFERENCES ticket_sales (id),
    -- The Sale Invoice a Credit Note credits, and the reversal route that
    -- made it owed — the Credit Note's motivo.
    ADD COLUMN credits_invoice_id UUID REFERENCES invoicing_invoices (id),
    ADD COLUMN reversal_reason TEXT CHECK (reversal_reason IN ('customer', 'platform', 'import_undo', 'staff_reversal', 'correction')),
    -- The IVA rate the whole document was priced under, stated once on the
    -- document so a reader never infers a platform rule from its lines. NULL
    -- on a manual document, whose lines each carry their own.
    ADD COLUMN iva_rate TEXT CHECK (iva_rate IN ('15', '0', 'exento', 'no_objeto')),
    -- When the authorized document was mailed to the buyer; NULL until then,
    -- and forever on a manual document, which the operator hands over.
    ADD COLUMN delivered_at TIMESTAMPTZ,
    -- When the Drainer should next work this document, on its backoff
    -- ladder; NULL when nothing is due (manual documents, and every terminal
    -- state).
    ADD COLUMN next_attempt_at TIMESTAMPTZ,

    ALTER COLUMN issuer_id DROP NOT NULL,
    ALTER COLUMN environment DROP NOT NULL,
    ALTER COLUMN issued_on DROP NOT NULL,
    ALTER COLUMN issued_at DROP NOT NULL,
    ALTER COLUMN issued_by DROP NOT NULL,
    ALTER COLUMN issuer_snapshot DROP NOT NULL,
    ALTER COLUMN signed_xml DROP NOT NULL,

    DROP CONSTRAINT invoicing_invoices_status_check,
    ADD CONSTRAINT invoicing_invoices_status_check
        CHECK (status IN ('owed', 'pending', 'authorized', 'not_authorized', 'rejected', 'needs_attention', 'withdrawn', 'annulled')),

    -- Signed whole or not at all.
    ADD CONSTRAINT invoicing_invoices_signed_whole
        CHECK ((signed_xml IS NULL) = (issuer_id IS NULL)
           AND (signed_xml IS NULL) = (environment IS NULL)
           AND (signed_xml IS NULL) = (issued_on IS NULL)
           AND (signed_xml IS NULL) = (issued_at IS NULL)
           AND (signed_xml IS NULL) = (issued_by IS NULL)
           AND (signed_xml IS NULL) = (issuer_snapshot IS NULL)),
    -- Only a document that was never signed can be in a state that says so.
    ADD CONSTRAINT invoicing_invoices_unsigned_states
        CHECK (signed_xml IS NOT NULL OR status IN ('owed', 'needs_attention', 'withdrawn')),
    -- A sale document or a credit note names its Sale; a manual one never
    -- does. A credit note, and only a credit note, names what it credits
    -- and why. A document the platform priced states its rate.
    ADD CONSTRAINT invoicing_invoices_kind_sale
        CHECK ((kind = 'manual') = (ticket_sale_id IS NULL)),
    ADD CONSTRAINT invoicing_invoices_kind_credit
        CHECK ((kind = 'credit_note') = (credits_invoice_id IS NOT NULL)
           AND (kind = 'credit_note') = (reversal_reason IS NOT NULL)),
    ADD CONSTRAINT invoicing_invoices_kind_rate
        CHECK ((kind = 'manual') = (iva_rate IS NULL));

-- What the Drainer claims: documents with work due, oldest due first.
CREATE INDEX invoicing_invoices_due
    ON invoicing_invoices (next_attempt_at)
    WHERE next_attempt_at IS NOT NULL;

-- A Sale's documents, for the Customer Area and the operator's Sale lookup.
CREATE INDEX invoicing_invoices_by_sale
    ON invoicing_invoices (ticket_sale_id)
    WHERE ticket_sale_id IS NOT NULL;

-- The list orders by when a document came to exist for the operator: its
-- emission instant once signed, its owing instant before. Replaces 096's
-- issued_at index, which an owed document could not be found in.
DROP INDEX invoicing_invoices_newest_first;
CREATE INDEX invoicing_invoices_newest_first
    ON invoicing_invoices ((COALESCE(issued_at, created_at)) DESC, created_at DESC, id DESC);
