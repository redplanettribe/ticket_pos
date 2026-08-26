-- The Sale Invoice Reissue (#483, parent #478, ADR 0061).
--
-- A Platform Operator corrects an authorized Sale Invoice's Recipient by
-- owing, in one transaction, a Credit Note against it and a fresh Sale
-- Invoice to the corrected Recipient. The fresh document is the one that
-- knows it is a reissue: it names the factura it SUPERSEDES, and it carries
-- who reissued, when, and the operator's optional note. Nothing is written
-- on the superseded factura — "superseded" is a relation read off this
-- link, never a status: the old document stays authorized, on file, a legal
-- artifact the buyer received, and no column of it moves.
--
-- "CURRENT SALE INVOICE" IS DERIVED, NOT STORED. The Sale's current factura
-- is its `sale`-kind document with no live successor (one not withdrawn)
-- that is not itself withdrawn or annulled. A column recording it would be
-- a second place for the same fact to be kept and a first place for it to
-- disagree; the partial unique index below is what holds the invariant
-- instead: a factura has at most one live successor, so a chain never
-- forks. A successor that was withdrawn — its Credit Note died before it
-- was signed (#484) — leaves the old factura current and the way open for
-- another reissue, which is why withdrawn rows are outside the index.
--
-- The trail — reissued_by, reissued_at — is stored by email and instant the
-- way issued_by, annulled_by and house_designated_by are (ADR 0015), and
-- set exactly when supersedes_invoice_id is. The note is bounded as an
-- Operator Reversal's is (500 characters), NULL when none was left, never
-- an empty string.
ALTER TABLE invoicing_invoices
    ADD COLUMN supersedes_invoice_id UUID REFERENCES invoicing_invoices (id),
    ADD COLUMN reissued_by TEXT CHECK (reissued_by IS NULL OR reissued_by <> ''),
    ADD COLUMN reissued_at TIMESTAMPTZ,
    ADD COLUMN reissue_note TEXT CHECK (reissue_note IS NULL OR (reissue_note <> '' AND char_length(reissue_note) <= 500)),

    -- Only a Sale Invoice supersedes, and only a Sale Invoice is superseded:
    -- the manual document and the Credit Note never take part.
    ADD CONSTRAINT invoicing_invoices_supersedes_sale_only
        CHECK (supersedes_invoice_id IS NULL OR kind = 'sale'),
    -- The trail travels with the link, whole or not at all.
    ADD CONSTRAINT invoicing_invoices_reissue_whole
        CHECK ((supersedes_invoice_id IS NULL) = (reissued_by IS NULL)
           AND (supersedes_invoice_id IS NULL) = (reissued_at IS NULL)
           AND (reissue_note IS NULL OR supersedes_invoice_id IS NOT NULL));

-- One live successor per factura: the chain never forks.
CREATE UNIQUE INDEX invoicing_invoices_one_live_successor
    ON invoicing_invoices (supersedes_invoice_id)
    WHERE supersedes_invoice_id IS NOT NULL AND status <> 'withdrawn';
