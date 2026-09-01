-- Abandon a document the authority refuses by number (#578, parent #575,
-- ADR 0068).
--
-- A NINTH STATUS, AND A TERMINAL ONE. `abandoned` says the document was
-- SENT, the Tax Authority never took it, and never will: it was never a
-- legal document, nothing is owed at the authority's portal and nothing is
-- ever declared for it. That is a different claim from the two deaths the
-- schema already knows, which is why it is a status and not `annulled` with
-- a reason column: `withdrawn` was never sent at all, and `annulled` was
-- held by the authority and then disowned by hand at its portal, which
-- leaves an obligation there. A reader must be able to conclude the right
-- thing from the one word.
--
-- THE ROW IS OTHERWISE UNTOUCHED, FOREVER. The number, the clave de acceso,
-- the signed bytes and every attempt stay exactly as they are — this table
-- is append-only for anything issued (ADR 0059) — and the secuencial stays
-- consumed: the sequence only ever moves forward, and an abandoned number
-- is never handed out again. A sequence with visible gaps is the correct
-- shape here, and the abandonment's author, instant, note and the Check
-- that precedes it are the platform's account of each gap.
--
-- No new state is needed for "signed": `invoicing_invoices_unsigned_states`
-- (098) already admits only owed, needs_attention and withdrawn without
-- signed bytes, so leaving `abandoned` out of it holds the rule that only a
-- document that reached the authority can be abandoned.
--
-- ABANDONED_BY / ABANDONED_AT / ABANDON_NOTE are the act's trail, in the
-- shape 099 gave the annulment's and 102 the reissue's: the operator's
-- email (ADR 0015), the instant, and an optional note bounded at 500
-- characters — "not registered at the portal, confirmed by phone" — NULL
-- when none was left and never an empty string. Deliberately NOT the
-- annulment's columns: 099's CHECK ties annulled_at to the annulled status,
-- and more to the point a reader who finds annulled_at set must be able to
-- conclude a portal annulment happened. Whole or not at all, and set
-- exactly when the row is abandoned; the note may stand alone missing.
ALTER TABLE invoicing_invoices
    DROP CONSTRAINT invoicing_invoices_status_check,
    ADD CONSTRAINT invoicing_invoices_status_check
        CHECK (status IN ('owed', 'pending', 'authorized', 'not_authorized', 'rejected', 'needs_attention', 'withdrawn', 'annulled', 'abandoned')),

    ADD COLUMN abandoned_by TEXT CHECK (abandoned_by IS NULL OR abandoned_by <> ''),
    ADD COLUMN abandoned_at TIMESTAMPTZ,
    ADD COLUMN abandon_note TEXT CHECK (abandon_note IS NULL OR (abandon_note <> '' AND char_length(abandon_note) <= 500)),

    ADD CONSTRAINT invoicing_invoices_abandonment_whole
        CHECK ((abandoned_by IS NULL) = (abandoned_at IS NULL)
           AND (status = 'abandoned') = (abandoned_at IS NOT NULL)
           AND (abandon_note IS NULL OR abandoned_at IS NOT NULL));
