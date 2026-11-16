-- The Annulment Notice's state and retry slot (#748, spec #742, ADR 0078).
--
-- The buyer of a Sale Invoice is told, once, that a factura they were sent
-- was annulled at the SRI. The notice is a fact about the annulment, not
-- about the document, so its state lives on the annulment it follows:
--
--   owed      Mark annulled was recorded on a Sale Invoice the buyer was
--             sent (its delivered_at stood). Held while the annulment's
--             Annulment Check is checking or at mismatch, due once it is
--             confirmed or unconfirmed: held and due are read off the check,
--             never written, so a verdict that moves needs no second write
--             to keep them true.
--   sent      the sender took it, at notice_sent_at. Never sent again.
--   dropped   a Reinstate withdrew it before it was sent (the Reinstate
--             ticket writes it; nothing does yet).
--   not_owed  a manual Tax Invoice, a factura the buyer was never sent
--             (annulled from pending or needs_attention, or authorized and
--             never delivered), and every annulment recorded before this
--             migration: history is not re-litigated.
--
-- notice_next_attempt_at is the retry slot on the delivery's ladder after a
-- refused send, NULL until one; notice_leased_until hides a claimed notice
-- from a concurrent tick; notice_attempts counts the sends tried, and picks
-- the ladder's rung.
ALTER TABLE invoicing_annulments
    ADD COLUMN notice_state TEXT NOT NULL DEFAULT 'not_owed'
        CHECK (notice_state IN ('owed', 'sent', 'dropped', 'not_owed')),
    ADD COLUMN notice_sent_at TIMESTAMPTZ,
    ADD COLUMN notice_next_attempt_at TIMESTAMPTZ,
    ADD COLUMN notice_leased_until TIMESTAMPTZ,
    ADD COLUMN notice_attempts INTEGER NOT NULL DEFAULT 0 CHECK (notice_attempts >= 0),
    ADD CONSTRAINT invoicing_annulments_notice_sent CHECK (
        (notice_state = 'sent') = (notice_sent_at IS NOT NULL)
    ),
    ADD CONSTRAINT invoicing_annulments_notice_slot CHECK (
        notice_state = 'owed' OR (notice_next_attempt_at IS NULL AND notice_leased_until IS NULL)
    );

CREATE INDEX invoicing_annulments_notice_owed
    ON invoicing_annulments (notice_next_attempt_at)
    WHERE notice_state = 'owed' AND reinstated_at IS NULL;
