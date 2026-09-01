-- Edition lineage: every published edition of the Privacy Policy and of the
-- Términos y Condiciones becomes COUNTABLE (#560, parent #556, ADR 0067).
--
-- Until now an edition had a `label` and nothing else to say about where it came
-- from, and "who owes an acceptance" was equality against the single current
-- row. Two integers replace both.
--
--   generation  which edition this descends from. A GATING edition — one that
--               re-gates the customer base and the staff platform — takes the
--               next generation.
--   revision    0 for a gating edition; 1, 2, 3 … for a CORRECTION published
--               against that generation. FLAT within the generation: a
--               correction to a correction is `1.2`, never `1.1.1`, because
--               what a reader needs from a label is what it descends from, and
--               nesting answers a question nobody asks.
--
-- AN EDITION IS GATING IF AND ONLY IF `revision = 0`. There is no `is_gating`
-- column, no `gating_from` column and no `published_as` column, and there must
-- never be one:
--
--   * #551 introduced `gating_from` to express a correction later PROMOTED to
--     gating, and #553 withdrew promotion altogether — so the column had
--     nothing left to express. An operator who wants a correction's text to
--     gate republishes those bytes as a new gating edition, which is legal
--     precisely because `content_hash` carries no UNIQUE (see below).
--   * `published_as` collapses into `revision`: `revision > 0` IS "published as
--     a correction".
--   * The gating fact is therefore not merely monotonic but IMMUTABLE — an
--     edition's answer to "did this re-gate anybody?" is fixed at publication
--     and no later act can change it. That is the stronger property, and it is
--     what lets the satisfying set be computed from the version rows alone with
--     nothing to maintain and nothing to backfill.
--
-- THE LABEL CARRIES LINEAGE ONLY. Reading `1.1` tells you what it descends
-- from and NOTHING about whether it still counts, or about what its publication
-- did to anybody. Status and kind are answered separately, so that no screen is
-- ever tempted to infer one from the other by parsing a string.
--
-- FORWARD-ONLY. Nothing here is destructive: two columns are added and every
-- existing label is left exactly as it was published.

-- ---------------------------------------------------------------------------
-- policy_versions
-- ---------------------------------------------------------------------------

ALTER TABLE policy_versions
    ADD COLUMN generation INTEGER,
    ADD COLUMN revision INTEGER;

-- Backfill from the label that was published, rather than from row order.
-- Renumbering by row order would rename edition `1` to `2` — the placeholder
-- sorts first — and an edition's name appears in support threads, in audits and
-- on the page a person read before accepting. It is a published fact.
--
-- `0-placeholder` (migration 060) yields generation 0, revision 0: it is a real
-- gating edition of its own generation, which is exactly what it was.
UPDATE policy_versions SET
    generation = substring(label from '^([0-9]+)')::int,
    revision = COALESCE(substring(label from '^[0-9]+\.([0-9]+)$')::int, 0);

DO $verify$
BEGIN
    IF EXISTS (SELECT 1 FROM policy_versions WHERE generation IS NULL) THEN
        RAISE EXCEPTION
            'policy_versions holds a label with no leading generation number; lineage cannot be backfilled from it';
    END IF;
END
$verify$;

ALTER TABLE policy_versions
    ALTER COLUMN generation SET NOT NULL,
    ALTER COLUMN revision SET NOT NULL,
    ADD CONSTRAINT policy_versions_generation_nonneg CHECK (generation >= 0),
    ADD CONSTRAINT policy_versions_revision_nonneg CHECK (revision >= 0);

-- Lineage is unambiguous: one row per (generation, revision). This, and not the
-- UNIQUE on `label`, is what makes "two editions cannot be given the same name"
-- true — the label is a RENDERING of these two integers, so uniqueness has to
-- hold where the meaning lives.
ALTER TABLE policy_versions
    ADD CONSTRAINT policy_versions_lineage_key UNIQUE (generation, revision);

-- The label is the frozen rendering of the lineage, and is never typed by a
-- human: `2` for a gating edition, `1.1` for a correction. The system renders
-- it (internal/consent/legal.Lineage.Label) and the database refuses anything
-- else, so there is no path — not an operator screen, not a migration, not a
-- psql session — by which an edition acquires a name that lies about its
-- descent.
--
-- NOT VALID, and only for `0-placeholder`: that one label was published in
-- migration 060 under the old freehand rule, before there was a rendering for
-- it to match. Rewriting it now would rename an edition that people accepted
-- and that migration 109's text is keyed on. Every row inserted from here on is
-- checked; the one grandfathered row is named in the comment instead of being
-- silently exempt.
ALTER TABLE policy_versions
    ADD CONSTRAINT policy_versions_label_is_lineage CHECK (
        label = generation::text || CASE WHEN revision = 0 THEN '' ELSE '.' || revision::text END
    ) NOT VALID;

-- ---------------------------------------------------------------------------
-- terms_versions — the parallel table (ADR 0066), the same two columns for the
-- same reasons. Parallel and NOT generalized: the two documents version
-- independently and an edition of one must never re-gate the other.
-- ---------------------------------------------------------------------------

ALTER TABLE terms_versions
    ADD COLUMN generation INTEGER,
    ADD COLUMN revision INTEGER;

UPDATE terms_versions SET
    generation = substring(label from '^([0-9]+)')::int,
    revision = COALESCE(substring(label from '^[0-9]+\.([0-9]+)$')::int, 0);

DO $verify$
BEGIN
    IF EXISTS (SELECT 1 FROM terms_versions WHERE generation IS NULL) THEN
        RAISE EXCEPTION
            'terms_versions holds a label with no leading generation number; lineage cannot be backfilled from it';
    END IF;
END
$verify$;

ALTER TABLE terms_versions
    ALTER COLUMN generation SET NOT NULL,
    ALTER COLUMN revision SET NOT NULL,
    ADD CONSTRAINT terms_versions_generation_nonneg CHECK (generation >= 0),
    ADD CONSTRAINT terms_versions_revision_nonneg CHECK (revision >= 0);

ALTER TABLE terms_versions
    ADD CONSTRAINT terms_versions_lineage_key UNIQUE (generation, revision);

-- VALIDATED here, unlike its policy counterpart: migration 105 seeded `1`,
-- which already renders correctly, so there is nothing to grandfather.
ALTER TABLE terms_versions
    ADD CONSTRAINT terms_versions_label_is_lineage CHECK (
        label = generation::text || CASE WHEN revision = 0 THEN '' ELSE '.' || revision::text END
    );

-- ---------------------------------------------------------------------------
-- content_hash MUST NEVER GAIN A UNIQUE
-- ---------------------------------------------------------------------------
--
-- Two editions with byte-identical text are LEGAL, and are the mechanism by
-- which an operator republishes a correction's words as a real gating edition:
-- the correction re-gated nobody, the republication re-gates everybody, and the
-- bytes are the same bytes. A UNIQUE on `content_hash` would make that publish
-- fail with a constraint violation, and the only workaround would be to change
-- the legal text in order to change the fingerprint — which is the tail wagging
-- the dog.
--
-- It is also wrong on its own terms: an edition published, corrected back to
-- its original wording, and published again is three honest rows with two
-- distinct fingerprints, and the duplicate is evidence, not a mistake.
--
-- This block fails the migration if anybody has already added one, so that the
-- rule is enforced by the schema's own history rather than by a comment.
DO $verify$
DECLARE
    offender TEXT;
BEGIN
    SELECT c.conname INTO offender
    FROM pg_constraint c
    JOIN pg_class t ON t.oid = c.conrelid
    JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY (c.conkey)
    WHERE t.relname IN ('policy_versions', 'terms_versions')
      AND c.contype IN ('u', 'p')
      AND a.attname = 'content_hash'
    LIMIT 1;

    IF offender IS NOT NULL THEN
        RAISE EXCEPTION
            'content_hash carries a uniqueness constraint (%): byte-identical editions are legal and must stay insertable',
            offender;
    END IF;

    SELECT i.relname INTO offender
    FROM pg_index x
    JOIN pg_class t ON t.oid = x.indrelid
    JOIN pg_class i ON i.oid = x.indexrelid
    JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY (x.indkey)
    WHERE t.relname IN ('policy_versions', 'terms_versions')
      AND x.indisunique
      AND a.attname = 'content_hash'
    LIMIT 1;

    IF offender IS NOT NULL THEN
        RAISE EXCEPTION
            'content_hash carries a unique index (%): byte-identical editions are legal and must stay insertable',
            offender;
    END IF;
END
$verify$;

-- No index on (generation, revision) beyond the UNIQUE above, and no index on
-- effective_date: both tables hold a handful of rows for the lifetime of the
-- product, and the satisfying set is computed by reading all of them.
