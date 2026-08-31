-- Policy Versions: one row per published edition of the Privacy Policy and its
-- Short Notice, with the fingerprint of the exact text a reader was shown
-- (#250, parent #249).
--
-- A Policy Acceptance is of a VERSION, never of "the policy" in the abstract
-- (CONTEXT.md). This table is what makes that sentence storable: it gives the
-- acceptance something to point at, and it gives a compliance officer answering
-- "what did this person actually agree to?" a row that answers rather than a
-- git log to reconstruct. Everything else about consent — the acceptance
-- timestamps on `customers`, the append-only `consent_records` — hangs off this
-- table and lands in #251.
--
-- READ THIS BEFORE INSERTING A ROW HERE. An INSERT into this table is not a
-- data edit, it is a DEPLOY WITH A BLAST RADIUS OF THE ENTIRE CUSTOMER BASE.
-- Once #251 lands, the prompt trigger is "no recorded acceptance of the CURRENT
-- Policy Version", and the current version is whichever row this table says it
-- is. So a new row instantly un-accepts every Customer who has ever accepted:
-- the next person to sign in is stopped for a checkbox, the next person to
-- check out cannot pay until they tick it, and there is no undo that does not
-- involve deleting evidence. That is the intended behaviour — a policy that
-- materially changed must be re-accepted — and it is precisely why a row is
-- added in a reviewed commit alongside the text it fingerprints, never by hand
-- against production. Restyling the wording of a published edition is the same
-- mistake wearing different clothes: the fingerprint stops matching the text,
-- which the build then refuses (see CONTENT_HASH below).
--
-- THE CURRENT VERSION is the one with the latest `effective_date` that has
-- arrived, ties broken by `created_at`. There is no `is_current` flag and no
-- `active` boolean, deliberately: a flag is a second place for the truth to
-- live and a way for two rows to claim to be current at once, or none to. A
-- future-dated row is therefore how an edition is scheduled — it sits here,
-- reviewed and merged, and becomes current on its own date without a deploy.
--
-- FORWARD-ONLY, as every migration here is. There is no down migration and this
-- one would be the worst possible candidate for one: dropping this table
-- orphans the acceptance evidence that references it.
CREATE TABLE policy_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The version label, as a human names the edition in a support thread or an
    -- audit ("which one did they accept?" — "0-placeholder"). UNIQUE because a
    -- label that named two editions would make that answer useless.
    --
    -- TEXT and not an integer, so the label can say what kind of edition it is.
    -- The seed below is called `0-placeholder` for exactly that reason.
    label TEXT NOT NULL UNIQUE,
    -- The day this edition takes effect. A DATE and not a timestamp: an
    -- effective date is a legal fact stated on the document itself, published to
    -- the day, and a reader comparing the page to this row must see the same
    -- thing they read on it. Nothing about consent needs it to the second.
    effective_date DATE NOT NULL,
    -- CONTENT_HASH: the SHA-256, hex-encoded, of the whole artifact set that
    -- makes up this edition — the policy body, the Short Notice and the three
    -- consent labels, in every Locale the platform publishes.
    --
    -- The text it covers lives in the BACKEND, embedded at
    -- backend/internal/consent/policy/artifacts/{en,es}/, and is served to the
    -- Storefront through the public policy endpoint. That is what makes this
    -- column mean anything: the bytes hashed here are the same bytes the page
    -- renders, so this value is evidence of what a person was shown rather than
    -- a checksum of a file in a repository. Had the text stayed in the
    -- Storefront's message catalogs, no Go test could have recomputed it and
    -- the two would have drifted silently. The package doc comment on
    -- `policy.go` argues the point in full; ADR 0036 records it, including why
    -- it is a narrow and deliberate exception to ADR 0027.
    --
    -- 64 hex characters, CHECKed. The check is cheap and the failure it catches
    -- is expensive: a truncated or mis-encoded fingerprint proves nothing and
    -- looks exactly like one that does.
    content_hash TEXT NOT NULL
        CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- No index beyond the primary key and the UNIQUE label. This table holds a
-- handful of rows for the lifetime of the product — editions of a privacy
-- policy are published in years, not in minutes — and the current-version
-- lookup that runs on the hot path reads all of them and picks one.

-- The placeholder edition.
--
-- SEEDED, and the seeding is itself consequential — see the warning above. It
-- is here rather than left to the legal drop because #251 needs a current
-- version to point acceptances at, and because a consent gate whose current
-- version is NULL has no defined behaviour worth writing.
--
-- `0-placeholder` says out loud what it is. The text it fingerprints is
-- deliberate placeholder prose still carrying visible bracketed markers —
-- `[DIRECCIÓN]`, the retention periods, the supervisory authority. The
-- controller, its RUC and its data protection contact are real; nothing else
-- here is, and it is not legal advice and has not
-- been reviewed by counsel. The real legal text is a content drop before
-- go-live; publishing it means adding a SECOND row here — label `1`, its own
-- effective date, the fingerprint of the real artifacts — in the same commit
-- that replaces the placeholder text. This row is never edited into the real
-- one: the whole point of an edition is that it stays readable as it was.
--
-- The effective date is backdated to the day the table shipped rather than set
-- to the day the migration happens to run, so that every environment agrees on
-- when this edition became current, and so that a deployment in any timezone
-- finds it already in effect rather than pending until midnight.
--
-- THE HASH BELOW IS THE ONE PRODUCTION HOLDS, and it is the fingerprint of the
-- placeholder text as this edition was actually published: the text that
-- migration 109 now carries as rows under the label `0-placeholder`.
--
-- IT WAS CORRECTED IN PLACE, ONCE, BY #558, AND THIS IS THE ONLY KIND OF EDIT
-- THAT IS EVER LEGITIMATE HERE. The placeholder prose was edited three times
-- after this migration had already been applied to production — to name the
-- real controller, its RUC and its data protection contact — and each time the
-- literal in this file was rewritten to match the new text. Production had
-- long since inserted its row, so those rewrites reached nothing but databases
-- created afterwards, and the same edition ended up with different fingerprints
-- in different databases: production's row says
-- 42d9c2c7…, while this file had drifted to a value no
-- row anywhere carries.
--
-- Restoring the value production holds is not "rewriting a hash". THE HASH ON A
-- ROW IS NEVER REWRITTEN — that row is the record of what eleven Customers were
-- shown, and #558's migration proves the text it stores against it rather than
-- the other way round. What is corrected here is a SEED literal, which reaches
-- only databases that have not yet applied migration 060 (the runner keys
-- `schema_migrations` on the filename), so that a database built from scratch
-- today reproduces production's editions instead of inventing its own.
--
-- The text this value fingerprints no longer exists in this repository as
-- files. It lives in migration 109, and
-- backend/internal/consent/legal/preimage_test.go recomputes THIS literal from
-- it — so the guarantee the deleted seed_test.go gave for the newest edition
-- alone now covers every edition, including this one.
INSERT INTO policy_versions (label, effective_date, content_hash)
VALUES (
    '0-placeholder',
    DATE '2026-08-01',
    '42d9c2c79c523359033bac14abcfea0c28a808e980e496e7b2d66979d55f0a2b'
);
