-- The documents that need an operator (#477, parent #471, ADR 0060).
--
-- Two facts the Operator Dashboard's queue and Mark annulled need that no
-- column held: WHEN a document was parked needs_attention, and WHO recorded
-- its annulment and when. Nothing here is a state — `status` stays the one
-- word the row is read by — and nothing here is ever deleted.
--
-- ATTENTION_SINCE, not updated_at. A document parked by the 24-hour rule is
-- still polled hourly and its updated_at moves with every poll, so "since
-- when" read off it would say "an hour ago" forever. This column is set
-- the moment the row ENTERS needs_attention, kept while it stays there, and
-- cleared when it leaves — healed by a late AUTORIZADO, annulled by the
-- operator — so the queue orders by the one instant that means "how long
-- has this been waiting". The CHECK holds the column to the state, so a row
-- can never be needs_attention without a since, nor carry a since in any
-- other state.
--
-- ANNULLED_BY / ANNULLED_AT are the operator's trail for a manual portal
-- act the platform cannot see (the SRI offers no web service for
-- annulment): who said "I annulled this at the portal", and when. Stored by
-- email, the way issued_by, house_designated_by and Payouts' recorded_by
-- are (ADR 0015). Both set or both null, and set exactly when the row is
-- annulled — the same shape as the House designation's pair (097). The row
-- keeps its number, its clave and its signed XML: the annulment is a fact
-- about a document that existed, and the record of it must outlive the act.
ALTER TABLE invoicing_invoices
    ADD COLUMN attention_since TIMESTAMPTZ,
    ADD COLUMN annulled_by TEXT CHECK (annulled_by IS NULL OR annulled_by <> ''),
    ADD COLUMN annulled_at TIMESTAMPTZ,

    ADD CONSTRAINT invoicing_invoices_attention_since_with_state
        CHECK ((status = 'needs_attention') = (attention_since IS NOT NULL)),
    ADD CONSTRAINT invoicing_invoices_annulment_whole
        CHECK ((annulled_by IS NULL) = (annulled_at IS NULL)
           AND (status = 'annulled') = (annulled_at IS NOT NULL));

-- The queue: documents needing attention, longest waiting first.
CREATE INDEX invoicing_invoices_needs_attention
    ON invoicing_invoices (attention_since)
    WHERE status = 'needs_attention';
