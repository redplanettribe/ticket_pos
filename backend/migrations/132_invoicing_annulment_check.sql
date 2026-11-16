-- The Annulment Check's state and queue slot (#746, spec #742, ADR 0078).
--
-- After Mark annulled on a factura the platform asks the SRI's
-- ConsultaComprobante on a schedule (+1, +5, +15, +30, +60 min, then every
-- 6 h until the later of recording + 7 days and the Annulment Window's
-- close) and reaches a verdict: confirmed, mismatch or unconfirmed. The
-- check lives on the annulment it is about, not on the invoice row, because
-- the invoice row's own next_attempt_at is the Drainer's queue and an
-- annulled document is off it.
--
-- check_verdict is NULL for an annulment recorded BEFORE this migration: it
-- has no check, reads as unconfirmed and is never scheduled, so history is
-- not re-litigated. Every annulment recorded from now on starts `checking`
-- with its first ask due at +1 min (the service writes both).
--
-- next_ask_at is the queue slot the Drainer's tick claims from, NULL when no
-- ask is due ever again (confirmed, past the schedule's end, or no check);
-- check_leased_until hides a claimed check from a concurrent tick, as the
-- invoice row's claim lease does. check_verdict_at is the instant of the
-- ask that reached the verdict, NULL while checking.
ALTER TABLE invoicing_annulments
    ADD COLUMN check_verdict TEXT CHECK (check_verdict IN ('checking', 'confirmed', 'mismatch', 'unconfirmed')),
    ADD COLUMN check_verdict_at TIMESTAMPTZ,
    ADD COLUMN next_ask_at TIMESTAMPTZ,
    ADD COLUMN check_leased_until TIMESTAMPTZ,
    ADD COLUMN asks_made INTEGER NOT NULL DEFAULT 0 CHECK (asks_made >= 0),
    ADD CONSTRAINT invoicing_annulments_check_whole CHECK (
        check_verdict IS NOT NULL
        OR (next_ask_at IS NULL AND check_verdict_at IS NULL AND check_leased_until IS NULL AND asks_made = 0)
    ),
    ADD CONSTRAINT invoicing_annulments_verdict_at CHECK (
        check_verdict IS NULL OR ((check_verdict = 'checking') = (check_verdict_at IS NULL))
    ),
    ADD CONSTRAINT invoicing_annulments_confirmed_ends CHECK (
        check_verdict IS DISTINCT FROM 'confirmed' OR next_ask_at IS NULL
    );

CREATE INDEX invoicing_annulments_check_due
    ON invoicing_annulments (next_ask_at)
    WHERE next_ask_at IS NOT NULL AND reinstated_at IS NULL;

-- One row per ask of an annulment's check: when, what it read, how many
-- calls it took and whether an operator's Check status made it rather than
-- the schedule. The verdict is the pure rule over these rows
-- (invoicing.AnnulmentVerdictOf); the calls themselves, with the SRI's raw
-- messages, are on invoicing_attempts as `status` rows.
CREATE TABLE invoicing_annulment_asks (
    id BIGSERIAL PRIMARY KEY,
    annulment_id UUID NOT NULL REFERENCES invoicing_annulments (id),
    asked_at TIMESTAMPTZ NOT NULL,
    reading TEXT NOT NULL CHECK (reading IN ('annulled', 'authorized', 'none')),
    manual BOOLEAN NOT NULL,
    calls INTEGER NOT NULL CHECK (calls BETWEEN 1 AND 3),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX invoicing_annulment_asks_annulment
    ON invoicing_annulment_asks (annulment_id, asked_at, id);
