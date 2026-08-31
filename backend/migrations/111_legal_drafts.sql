-- The Legal Center's drafting tables (#561, spec #556, map #540).
--
-- Migration 109 moved the published legal text into the database. This one adds
-- the place an edition is WRITTEN before it is published: one mutable draft per
-- document, held server-side.
--
-- WHY SERVER-SIDE AT ALL. A draft of the Privacy Policy is ~280 lines of prose
-- in two languages, written across several sittings by one person. Held in the
-- browser it would live exactly as long as a tab: a reload, a laptop closing, a
-- colleague's machine, and an afternoon's drafting is gone. Held here it is the
-- same draft from any browser the operator signs in on, and there is exactly one
-- of it, which is what makes "discard and start again from the current edition"
-- a meaningful act rather than a per-tab accident.
--
-- ONE ROW PER DOCUMENT — the primary key IS the document. Not one draft per
-- operator, and not a history of drafts. Two people editing the Privacy Policy
-- into two private drafts would publish one of them and silently lose the other;
-- a legal document has one text at a time, and the draft is the platform's, not
-- its author's. There is deliberately no four-eyes rule and no draft workflow
-- (map #540): the last save wins, `updated_by`/`updated_at` say who touched it
-- last, and the ceremony lives at publish (#563).
--
-- NOTHING HERE IS EVIDENCE. No draft text is ever hashed, shown to a reader, or
-- pointed at by an acceptance. That is the line between these tables and 109's:
-- those rows exist so a fingerprint can be recomputed from the database years
-- from now, these rows exist so somebody can go to lunch. Their SHAPE is
-- deliberately the same anyway, because publishing (#563) should be a copy
-- rather than a translation.
--
-- FORWARD-ONLY, like every migration here. Dropping these would lose only work
-- in progress, but there is still no down migration: that is the house rule.

-- One draft per document.
CREATE TABLE legal_drafts (
    -- Which document is being drafted. The PRIMARY KEY, so "one mutable draft
    -- per document" is a fact the database enforces rather than a convention
    -- that an upsert can forget.
    --
    -- TEXT with a CHECK rather than an enum, matching `locale` and `slug` on the
    -- artifact tables: the two documents are parallel and independent (ADR 0066)
    -- and the set of them changes about once a decade.
    document TEXT PRIMARY KEY CHECK (document IN ('policy', 'terms')),
    -- The edition this draft was opened from: the id of the `policy_versions` or
    -- `terms_versions` row that was current when the operator started.
    --
    -- NO FOREIGN KEY, deliberately, and it is the one column here that wants
    -- explaining. It points at one of TWO tables depending on `document`, and a
    -- polymorphic FK is not a thing Postgres has; two nullable columns with two
    -- FKs would buy integrity for a value that is not evidence. It is here so
    -- the editor can say "the current edition has moved on since you started" —
    -- a comparison against whatever is current NOW — and a dangling id answers
    -- that question exactly as well as a live one. Version rows are never
    -- deleted in any case: both artifact FKs in 109 are ON DELETE RESTRICT.
    base_version_id UUID NOT NULL,
    -- Who saved it last, by email, and when. NOT authorship of an edition —
    -- that is stamped at publish (#563) — but the answer to "who else is in
    -- here?" when two operators reach for the same document in the same week.
    updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The languages this draft intends to PUBLISH in.
--
-- A TABLE AND NOT A TEXT[] OR A JSONB COLUMN, on migration 072's reasoning: a
-- set the database can constrain, join and count beats an opaque value nothing
-- can check. ON DELETE CASCADE, because discarding a draft discards all of it.
--
-- EXPLICIT, AND NEVER INFERRED FROM WHICH CELLS ARE FILLED. That is the whole
-- point of the table. A half-translated language must be a draft that CANNOT
-- publish, not a language quietly dropped because a textarea was empty, and
-- inference makes those two indistinguishable. The set is bounded by the
-- platform's app locales — `platform.ParseLocale` is a closed switch — so it is
-- a menu and not free text; the service enforces that rather than a CHECK here,
-- because a CHECK would need a migration to add a language and the point of 109
-- was that publishing stops needing a deploy.
CREATE TABLE legal_draft_locales (
    document TEXT NOT NULL REFERENCES legal_drafts (document) ON DELETE CASCADE,
    locale TEXT NOT NULL,
    PRIMARY KEY (document, locale)
);

-- The draft text: one row per (document, artifact, language), which is exactly
-- `policy_version_artifacts` with the version id swapped for the document.
CREATE TABLE legal_draft_artifacts (
    document TEXT NOT NULL REFERENCES legal_drafts (document) ON DELETE CASCADE,
    -- The language this cell is written in. A cell the operator has not written
    -- yet has NO ROW: absent and empty are the same thing in a draft, and the
    -- completeness rule counts both as a gap.
    locale TEXT NOT NULL,
    -- Which artifact this is, in the vocabulary the surfaces use
    -- ('short-notice', 'label-marketing-consent', 'policy', 'terms', …). Free
    -- text on purpose, as it is in 109: adding an artifact is the operator's act
    -- and must not need a deploy.
    slug TEXT NOT NULL,
    -- The artifact's position in the fingerprint preimage (#541), carried per
    -- row exactly as 109 carries it. The service writes one ordinal per slug
    -- across every language of the draft, from the single ordered slug list the
    -- editor holds; it is repeated per row rather than kept in a fourth table so
    -- that publishing is a row copy.
    ordinal INTEGER NOT NULL,
    -- The text, markdown, trimmed. UNLIKE 109 THERE IS NO NON-EMPTY CHECK: an
    -- artifact half-written at 6pm must survive being saved, and refusing to
    -- store an empty cell would mean the editor silently dropped the operator's
    -- half-finished thought. What an empty cell may not do is PUBLISH, and that
    -- is the completeness rule's job (#563), not this column's.
    body TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (document, locale, slug),
    -- One artifact per position within a language, as in 109. Positions need not
    -- be contiguous: a slug not yet written in Spanish leaves a hole in the
    -- Spanish ordinals, and the preimage cares about ORDER, not about counting.
    UNIQUE (document, locale, ordinal)
);

COMMENT ON TABLE legal_drafts IS
    'One mutable, server-held draft per legal document (#561). Work in progress, never evidence: nothing here is hashed or shown to a reader.';
