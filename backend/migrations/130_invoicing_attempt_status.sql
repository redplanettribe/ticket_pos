-- The attempts ledger records the Tax Authority's status service too (#744,
-- spec #742, ADR 0078).
--
-- Migration 096 admitted two operations: `submit` (SRI recepción) and
-- `query` (SRI autorización). An annulled factura is now asked about through
-- a third service, the SRI's ConsultaComprobante, and every call is a row
-- here under its own operation, `status`. It is a new word and not `query`
-- on purpose: Abandon's fresh-Check rule reads the last two `query` rows
-- (ADR 0068 as amended), and a status call must never be one of them.
--
-- A `status` row's outcome is its READING, one of exactly three: `annulled`
-- and `authorized` are the authority's positive answers, and `none` is
-- everything else - a refusal, an unrecognised state, a redirect, a non-SOAP
-- body, a reset, a timeout. A call that failed in transport is `none` with
-- its error, never `error`, because no answer from this service is ever a
-- failure of the caller. `none` is not `unknown`: `unknown` is autorización
-- saying it holds nothing under the clave, and nothing reads a `none` as that.
--
-- The CHECKs are widened and nothing else changes; no existing row is
-- touched. The pairing constraint keeps the two vocabularies apart, so a
-- `query` row can never carry a reading and a `status` row never an
-- autorización outcome.
ALTER TABLE invoicing_attempts
    DROP CONSTRAINT invoicing_attempts_operation_check,
    ADD CONSTRAINT invoicing_attempts_operation_check
        CHECK (operation IN ('submit', 'query', 'status')),
    DROP CONSTRAINT invoicing_attempts_outcome_check,
    ADD CONSTRAINT invoicing_attempts_outcome_check
        CHECK (outcome IN ('received', 'authorized', 'not_authorized', 'rejected', 'unknown', 'error', 'annulled', 'none')),
    ADD CONSTRAINT invoicing_attempts_status_reading_check
        CHECK (CASE WHEN operation = 'status'
                    THEN outcome IN ('annulled', 'authorized', 'none')
                    ELSE outcome NOT IN ('annulled', 'none') END);
