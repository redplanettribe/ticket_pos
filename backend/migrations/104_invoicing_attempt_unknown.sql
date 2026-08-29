-- "The authority has no record of this reference" is its own outcome on the
-- attempts ledger (#514, parent #513).
--
-- Migration 096 admitted five outcomes, and the Ecuador adapter mapped an
-- autorización answer with numeroComprobantes 0 onto `received` — the same
-- word it uses for EN PROCESAMIENTO. The two are opposites: `received` is the
-- authority saying it holds the document, `unknown` is it saying it has never
-- had one under this clave, which is where a recepción call that died in
-- transport leaves a document. Conflating them made the platform's first
-- production factura poll for a day against a clave the SRI had never seen,
-- while the staff page told the operator "the SRI has this document".
--
-- The CHECK is widened and nothing else changes. Existing rows keep their
-- values: a `received` row written for an unknown clave before this migration
-- stays as it is, and is not rewritten. Nothing in the platform reads history
-- to decide the truth of it — a fresh query answers for itself — so there is
-- no backfill to do and nothing to guess at.
ALTER TABLE invoicing_attempts
    DROP CONSTRAINT invoicing_attempts_outcome_check,
    ADD CONSTRAINT invoicing_attempts_outcome_check
        CHECK (outcome IN ('received', 'authorized', 'not_authorized', 'rejected', 'unknown', 'error'));
