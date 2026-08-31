-- Cancelling a scheduled edition: the night between the click and the rollover
-- (#564, spec #556, ADR 0067).
--
-- Migration 112 made a publication ACCOUNTABLE. This one makes the ONE part of
-- it that is not yet irreversible undoable — and marks the undoing.
--
-- WHY THERE IS A NIGHT AT ALL. A gating edition may not take effect the day it
-- is published (#563): the service refuses an effective date earlier than
-- tomorrow, and later than any edition already published. That refusal is the
-- substitute for a second pair of eyes — production holds ONE platform_operators
-- row, so a two-person rule would deadlock on every act — and a delay is only a
-- substitute for review if something can actually be done during it. Until this
-- migration the row was already written and nothing could unwrite it, so the
-- night bought a feeling rather than a chance. These two columns are the chance.
--
-- CANCELLING IS UNGATED AND IMMEDIATE: no reason column, no approval column, no
-- delay of its own, and no confirmation ceremony in the interface above it.
-- UNDOING IS ALWAYS CHEAPER THAN DOING. The act it reverses is the one that
-- re-gates the entire customer base and the whole staff platform; the act of
-- withdrawing it, before anybody has been shown a word of it, moves nobody and
-- changes no page. Ceremony on this side would price the safe act like the
-- dangerous one and leave an operator publishing something they had changed
-- their mind about because unwinding it looked expensive.
--
-- WHY THERE IS NO `cancellation_reason`. Every other bounded note in this
-- schema — the Operator Reversal's, the reissue's, migration 112's
-- correction_reason — exists because a fact is UNRECOVERABLE from the rows
-- otherwise: why somebody thought the words were wrong cannot be read off the
-- bytes. A cancellation has no such fact. The edition survives in full, its
-- artifact rows and its fingerprint intact, so "what was nearly published" is
-- readable forever; the only thing left to say is that it was withdrawn, and
-- `cancelled_by` + `cancelled_at` say it. A required reason would also be a gate
-- on the cheap act, which is the one thing this feature must not have.
--
-- WHY THE ROW IS RETAINED AND MARKED, NEVER DELETED. A DELETE would leave no
-- trace that a re-gating of everybody was, for a night, about to happen — the
-- exact fact an audit of "what did this platform nearly do" is looking for. It
-- would also be a lie of a second kind: migration 110's UNIQUE (generation,
-- revision) would free the number, and a LATER edition would silently reuse the
-- label of one that had existed. Keeping the row keeps the label spent, which is
-- what makes a label mean one thing forever. legal.NextGating therefore counts
-- CANCELLED editions when it picks the next generation, and skips them
-- everywhere else.
--
-- THIS IS THE ONLY UPDATE THIS CODEBASE ISSUES AGAINST EITHER VERSION TABLE, and
-- it must stay the only one. "Nothing is ever mutated" (repository/publish.go)
-- is a rule about the EDITION: its label, its dates, its artifact rows and its
-- content hash are what acceptances fingerprint, and none of them is touched
-- here. What moves is a mark ABOUT the row, written once, on a row that by
-- construction no acceptance can name — an edition nobody has been shown,
-- because its day has not come.
--
-- HOW A CANCELLED EDITION STOPS COUNTING, AND WHY NOTHING FIRES. The gate is a
-- predicate over a date that moves on its own: `effective_date <= CURRENT_DATE`.
-- A cancelled edition is excluded by the SAME READ that answers that predicate —
-- `cancelled_at IS NULL` sits beside it in every current-edition selector, and
-- the lineage read carries the mark back so legal.GatingFloor and
-- legal.Satisfying skip the row. Nothing is deleted at midnight, no job runs and
-- no cache is invalidated, because there is nothing to fire: the query simply
-- stops choosing the row, exactly as it started choosing a scheduled one.
--
-- FORWARD-ONLY, like every migration here.

-- ---------------------------------------------------------------------------
-- The cancellation mark, added identically to both tables.
--
-- Parallel and NOT generalized (ADR 0066): the two documents version
-- independently, and a shared table is one place where withdrawing a Policy
-- edition and withdrawing a Terms edition could be confused for each other.
-- ---------------------------------------------------------------------------

ALTER TABLE policy_versions
    -- Who withdrew it, by email, exactly as published_by, issued_by and
    -- annulled_by are stored (ADR 0015, migration 112). No foreign key to
    -- platform_operators: the trail must survive an operator's removal from the
    -- allowlist, and a reference would either block the delete or erase history.
    ADD COLUMN cancelled_by TEXT
        CHECK (cancelled_by IS NULL OR cancelled_by <> ''),
    -- When, by the server clock that performed the act — a TIMESTAMPTZ, like
    -- published_at and for the same reason: this is the OPERATIONAL MOMENT, not
    -- a legal fact stated on a document. The date that decides whether the act
    -- was still possible is `effective_date`, read against CURRENT_DATE by the
    -- statement that writes these columns.
    ADD COLUMN cancelled_at TIMESTAMPTZ;

ALTER TABLE policy_versions
    -- The mark travels whole or not at all, in migration 112's house style. A
    -- row with an instant and no operator is a row whose reader has to guess
    -- which half is missing and why.
    ADD CONSTRAINT policy_versions_cancellation_whole
        CHECK ((cancelled_by IS NULL) = (cancelled_at IS NULL));

-- ---------------------------------------------------------------------------
-- terms_versions — the parallel table, the same two columns and the same
-- constraint, for the same reasons.
-- ---------------------------------------------------------------------------

ALTER TABLE terms_versions
    ADD COLUMN cancelled_by TEXT
        CHECK (cancelled_by IS NULL OR cancelled_by <> ''),
    ADD COLUMN cancelled_at TIMESTAMPTZ;

ALTER TABLE terms_versions
    ADD CONSTRAINT terms_versions_cancellation_whole
        CHECK ((cancelled_by IS NULL) = (cancelled_at IS NULL));

-- ---------------------------------------------------------------------------
-- NO CHECK SAYING A CANCELLED EDITION MUST BE IN THE FUTURE, and the omission is
-- deliberate rather than an oversight. Such a CHECK would have to read
-- CURRENT_DATE, which is not IMMUTABLE, so Postgres would refuse it outright —
-- and even if it did not, it would re-evaluate against a moving day and turn
-- every honest historical cancellation into a row the table could no longer
-- hold. The rule "the effective date must not have passed" belongs to the ACT,
-- and it is enforced where the act happens: the UPDATE's own WHERE clause names
-- `effective_date > CURRENT_DATE`, so the refusal is the same date predicate the
-- rest of this feature turns on and not a second opinion about it.
--
-- No index on either column, on migration 110's and 112's reasoning: both tables
-- hold a handful of rows for the lifetime of the product, and the mark is read
-- WITH the edition by a query that was reading every row anyway.
-- ---------------------------------------------------------------------------

COMMENT ON COLUMN policy_versions.cancelled_at IS
    'When a scheduled edition was withdrawn before its day (#564). The row is RETAINED and marked, never deleted: the record of what was nearly published survives, and the label stays spent. Excluded from every current-edition selector, from the gating floor and from the satisfying set by `cancelled_at IS NULL` in the same query that answers `effective_date <= CURRENT_DATE`.';
COMMENT ON COLUMN terms_versions.cancelled_at IS
    'When a scheduled edition was withdrawn before its day (#564). The row is RETAINED and marked, never deleted: the record of what was nearly published survives, and the label stays spent. Excluded from every current-edition selector, from the gating floor and from the satisfying set by `cancelled_at IS NULL` in the same query that answers `effective_date <= CURRENT_DATE`.';
