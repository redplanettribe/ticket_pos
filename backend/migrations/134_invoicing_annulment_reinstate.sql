-- Reinstate (#749, spec #742, ADR 0078 section 5).
--
-- When the Annulment Window has closed with the SRI still reporting a
-- factura authorized (its Annulment Check at mismatch), a Platform Operator
-- reinstates it: the factura returns to authorized and the annulment that
-- never took is CLOSED, never deleted. Migration 131 added reinstated_at,
-- unwritten, so that the partial unique index invoicing_annulments_one_open
-- was real from the start; this migration adds the rest of the stamp:
--
--   reinstated_by   the operator's email, from the session.
--   reinstate_note  the operator's optional note, at most 500 characters,
--                   NULL rather than empty when none was left, as every
--                   operator note on invoicing is.
--
-- reinstated_at and reinstated_by are written together or not at all, and a
-- note only on a reinstated annulment.
--
-- THE REINSTATEMENT NOTICE. A buyer who was already sent the Annulment
-- Notice holds the words "discard it"; when the Reinstate owes no Credit
-- Note (whose own mail would be what they hear), they are owed a mail
-- carrying the RIDE and XML again. It is a fact about the reinstatement, so
-- its state lives here beside it, shaped as the Annulment Notice's
-- (migration 133):
--
--   owed      the Annulment Notice was sent and no Credit Note follows.
--   sent      the sender took it, at reinstatement_notice_sent_at.
--   not_owed  every other reinstatement.
--
-- NULL on an annulment that stands, so a reinstatement always says which.
-- reinstatement_notice_next_attempt_at is its retry slot on the delivery's
-- ladder, reinstatement_notice_leased_until hides a claimed one from a
-- concurrent tick, and reinstatement_notice_attempts counts the sends tried.
ALTER TABLE invoicing_annulments
    ADD COLUMN reinstated_by TEXT CHECK (reinstated_by IS NULL OR reinstated_by <> ''),
    ADD COLUMN reinstate_note TEXT CHECK (reinstate_note IS NULL OR (reinstate_note <> '' AND char_length(reinstate_note) <= 500)),
    ADD COLUMN reinstatement_notice_state TEXT
        CHECK (reinstatement_notice_state IN ('owed', 'sent', 'not_owed')),
    ADD COLUMN reinstatement_notice_sent_at TIMESTAMPTZ,
    ADD COLUMN reinstatement_notice_next_attempt_at TIMESTAMPTZ,
    ADD COLUMN reinstatement_notice_leased_until TIMESTAMPTZ,
    ADD COLUMN reinstatement_notice_attempts INTEGER NOT NULL DEFAULT 0 CHECK (reinstatement_notice_attempts >= 0),
    ADD CONSTRAINT invoicing_annulments_reinstated_whole CHECK (
        (reinstated_at IS NULL) = (reinstated_by IS NULL)
        AND (reinstated_at IS NULL) = (reinstatement_notice_state IS NULL)
        AND (reinstated_at IS NOT NULL OR reinstate_note IS NULL)
    ),
    ADD CONSTRAINT invoicing_annulments_reinstatement_notice_sent CHECK (
        (reinstatement_notice_state = 'sent') = (reinstatement_notice_sent_at IS NOT NULL)
    ),
    ADD CONSTRAINT invoicing_annulments_reinstatement_notice_slot CHECK (
        reinstatement_notice_state = 'owed'
        OR (reinstatement_notice_next_attempt_at IS NULL AND reinstatement_notice_leased_until IS NULL)
    );

CREATE INDEX invoicing_annulments_reinstatement_notice_owed
    ON invoicing_annulments (reinstatement_notice_next_attempt_at)
    WHERE reinstatement_notice_state = 'owed';
