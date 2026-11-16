-- The annulments ledger (#745, spec #742, ADR 0078).
--
-- Mark annulled now admits an AUTHORIZED factura (a Sale Invoice or a manual
-- Tax Invoice) beside a pending or needs_attention one, and an annulment may
-- one day be reinstated and then recorded again. The invoice row's
-- annulled_by / annulled_at (migration 099) stay the CURRENT state, tied to
-- `status = 'annulled'` by invoicing_invoices_annulment_whole; this table is
-- the HISTORY, one row per Mark annulled on a factura, never deleted.
--
-- A Credit Note's Mark annulled writes no row here: it keeps exactly what
-- ADR 0061 and #484 built, with no window and no record beyond 099's pair.
--
-- What a row holds today: the factura, the state it was annulled from, who
-- and when, the operator's optional note (at most 500 characters, as every
-- operator note on invoicing is, and NULL rather than empty when none was
-- left) and whether the operator acknowledged, past the Annulment Window,
-- that the portal took the annulment all the same.
--
-- Later tickets of spec #742 add the Annulment Check's state and queue slot,
-- the Annulment Notice's state, the Unrecorded Annulment origin and the rest
-- of the reinstatement stamp. reinstated_at is here already, unwritten,
-- because the rule it carries is this ticket's: a factura has AT MOST ONE
-- annulment without a reinstatement, held by the partial unique index below,
-- so a second open annulment of one factura cannot exist whatever the code
-- above it does.
CREATE TABLE invoicing_annulments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id UUID NOT NULL REFERENCES invoicing_invoices (id),
    from_status TEXT NOT NULL CHECK (from_status IN ('pending', 'needs_attention', 'authorized')),
    annulled_by TEXT NOT NULL CHECK (annulled_by <> ''),
    annulled_at TIMESTAMPTZ NOT NULL,
    note TEXT CHECK (note IS NULL OR (note <> '' AND char_length(note) <= 500)),
    late_acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
    reinstated_at TIMESTAMPTZ CHECK (reinstated_at IS NULL OR reinstated_at >= annulled_at),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX invoicing_annulments_one_open
    ON invoicing_annulments (invoice_id)
    WHERE reinstated_at IS NULL;

CREATE INDEX invoicing_annulments_invoice
    ON invoicing_annulments (invoice_id, annulled_at);
