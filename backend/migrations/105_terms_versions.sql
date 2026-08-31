-- Terms Versions: one row per published edition of the Términos y Condiciones
-- Generales, with the fingerprint of the exact text a reader was shown (#535,
-- parent #533, ADR 0066).
--
-- A Terms Acceptance is of a VERSION, never of "the terms" in the abstract —
-- the sentence policy_versions (migration 060) made storable for the Privacy
-- Policy, made storable here for the contract. This table is PARALLEL TO
-- policy_versions AND DELIBERATELY NOT A GENERALIZATION OF IT: the two
-- documents version independently, an edition of one must never re-gate the
-- other, and the existing FK/CHECK chain hanging off policy_versions stays
-- untouched (ADR 0066). Sharing a table would buy one CREATE TABLE at the price
-- of a WHERE clause every future reader could forget.
--
-- READ MIGRATION 060'S WARNING BEFORE INSERTING A ROW HERE; it applies with a
-- WIDER BLAST RADIUS. An INSERT into this table re-gates the ENTIRE CUSTOMER
-- BASE at their next sign-in or checkout (#536, #537) and the ENTIRE STAFF
-- PLATFORM — every Organizer, Event Staff member and Platform Operator — at
-- their next sign-in (#538). That is the intended behaviour for a materially
-- changed contract (§33), and it is why a row is added in a reviewed commit
-- alongside the text it fingerprints, never by hand against production.
--
-- THE CURRENT VERSION is the one with the latest effective_date that has
-- arrived, ties broken by created_at — migration 060's rule, for its reasons:
-- no is_current flag, and a future-dated row is how an edition is scheduled.
--
-- FORWARD-ONLY, like every migration here, and the worst candidate for a down
-- migration there could be: dropping this table orphans acceptance evidence.
CREATE TABLE terms_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The edition label, as a human names it in a support thread or an audit.
    -- TEXT, UNIQUE, for migration 060's reasons.
    label TEXT NOT NULL UNIQUE,
    -- The day this edition takes effect: a DATE, a legal fact stated on the
    -- document itself.
    effective_date DATE NOT NULL,
    -- The SHA-256, hex-encoded, of the whole artifact set that makes up this
    -- edition — the Spanish body and the acceptance checkbox label, the single
    -- legally prevailing text (§37). The text lives embedded in the backend at
    -- backend/internal/consent/terms/artifacts/es/, is served by the public
    -- terms endpoint, and backend/internal/consent/terms/seed_test.go
    -- recomputes this value from THIS file — so the bytes hashed here are the
    -- bytes the page renders, and this column is evidence of what a person
    -- accepted rather than a checksum of a repository file.
    content_hash TEXT NOT NULL
        CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- No index beyond the primary key and the UNIQUE label: a handful of rows for
-- the lifetime of the product, and the current-version lookup reads them all
-- and picks one.

-- Edition 1: the real Términos y Condiciones Generales, seeded with the
-- machinery rather than after it (#533's ruling — no placeholder two-step).
-- THIS ROW IS THE ONE-TIME RE-GATE of the existing Customer base and, once
-- #538 lands, the gate every member of the Staff platform meets at their next
-- sign-in: nobody has accepted edition 1, so everybody owes it. Their standing
-- optional consents are not disturbed.
--
-- The effective date is the deploy date, as a LITERAL and not CURRENT_DATE,
-- for migration 060's reason: the day an edition became current is a fact
-- about the edition, and a computed date would give every environment a
-- different answer to a question an audit asks.
INSERT INTO terms_versions (label, effective_date, content_hash)
VALUES (
    '1',
    DATE '2026-08-30',
    '271ae893572d77824793dadf517cfd35f3afe34c43b7d914d385bb97e519c573'
);
