-- The Closing Sweep and the Unrecorded Annulment (#750, spec #742, ADR 0078).
--
-- Once a month, from day 8 of the following month through day 12
-- (America/Guayaquil), the Sale Invoice Drainer's tick asks the SRI's
-- ConsultaComprobante about every production factura of the month just
-- closed that the platform holds as authorized. ONE READING PER FACTURA IS
-- FINAL, because nothing can be annulled once its Annulment Window has
-- closed: AUTORIZADO marks it swept, ANULADO marks it swept and raises an
-- Unrecorded Annulment, and a no reading leaves it due again on a later
-- tick. The state lives on the invoice row so the dashboard's unswept count
-- and the archive's list are single queries.
--
-- Per-document sweep state:
--
--   sweep_month         day 1 of the month the reading was taken for (the
--                       month of issued_on), NULL until the first ask.
--   sweep_reading       'authorized' or 'annulled', the final reading; NULL
--                       while none has landed. A no reading is never stored
--                       here: its calls are on invoicing_attempts as
--                       `status` rows.
--   swept_at            when that reading landed; with sweep_reading.
--   sweep_asked_at      the last ask, whatever it read: a factura asked on
--                       a tick is not asked again on the same tick.
--   sweep_again_at      an operator's Sweep again: the factura is due once
--                       more, on the next tick, whatever the day.
--   sweep_leased_until  hides a claimed factura from a concurrent tick.
--
-- The Unrecorded Annulment: a factura the platform holds as authorized that
-- the SRI reports annulled. The platform NEVER records the annulment
-- itself; it flags the factura and queues it for an operator.
--
--   unrecorded_annulment_found_at       when the sweep's ANULADO landed.
--   unrecorded_annulment_attempt_id     the call that read it, on the ledger.
--   unrecorded_annulment_acknowledged_by / _at
--                                       an operator's acknowledgement,
--                                       where a live Credit Note refuses
--                                       Mark annulled: it clears the queue
--                                       entry; the flag stays.
--
-- Mark annulled on a flagged factura clears the flag (the annulment is now
-- recorded) and writes from_unrecorded on its annulments row, whose
-- Annulment Check starts confirmed with no asks: the SRI has said already.
ALTER TABLE invoicing_invoices
    ADD COLUMN sweep_month DATE CHECK (sweep_month IS NULL OR EXTRACT(DAY FROM sweep_month) = 1),
    ADD COLUMN sweep_reading TEXT CHECK (sweep_reading IN ('authorized', 'annulled')),
    ADD COLUMN swept_at TIMESTAMPTZ,
    ADD COLUMN sweep_asked_at TIMESTAMPTZ,
    ADD COLUMN sweep_again_at TIMESTAMPTZ,
    ADD COLUMN sweep_leased_until TIMESTAMPTZ,
    ADD COLUMN unrecorded_annulment_found_at TIMESTAMPTZ,
    ADD COLUMN unrecorded_annulment_attempt_id BIGINT REFERENCES invoicing_attempts (id),
    ADD COLUMN unrecorded_annulment_acknowledged_by TEXT
        CHECK (unrecorded_annulment_acknowledged_by IS NULL OR unrecorded_annulment_acknowledged_by <> ''),
    ADD COLUMN unrecorded_annulment_acknowledged_at TIMESTAMPTZ,
    ADD CONSTRAINT invoicing_invoices_swept_whole CHECK (
        (sweep_reading IS NULL) = (swept_at IS NULL)
        AND (sweep_reading IS NULL OR sweep_month IS NOT NULL)
    ),
    ADD CONSTRAINT invoicing_invoices_unrecorded_annulment_whole CHECK (
        (unrecorded_annulment_acknowledged_at IS NULL) = (unrecorded_annulment_acknowledged_by IS NULL)
        AND (unrecorded_annulment_found_at IS NOT NULL OR
             (unrecorded_annulment_attempt_id IS NULL AND unrecorded_annulment_acknowledged_at IS NULL))
    );

-- The sweep's population, read by the tick's claim and the dashboard's
-- count: production facturas not yet swept, by Emission Date.
CREATE INDEX invoicing_invoices_unswept
    ON invoicing_invoices (issued_on)
    WHERE environment = 'production' AND kind IN ('sale', 'manual') AND sweep_reading IS NULL;

-- The needs-attention queue's fourth door.
CREATE INDEX invoicing_invoices_unrecorded_annulment
    ON invoicing_invoices (unrecorded_annulment_found_at)
    WHERE unrecorded_annulment_found_at IS NOT NULL;

ALTER TABLE invoicing_annulments
    ADD COLUMN from_unrecorded BOOLEAN NOT NULL DEFAULT FALSE;
