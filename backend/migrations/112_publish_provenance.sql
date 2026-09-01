-- Publish provenance: who published an edition, when, what it changed, why —
-- and how many people it re-gated (#563, spec #556, ADR 0067).
--
-- Migration 110 made an edition COUNTABLE (generation, revision). This one makes
-- its PUBLICATION accountable. Until now a version row said what the edition is
-- and said nothing whatever about the act that created it: the two seeded rows
-- were written by migrations, and every row after them will be written by one
-- person pressing one button that re-gates the entire customer base.
--
-- WHY PROVENANCE LIVES ON THE VERSION ROW AND NOT IN A LOG.
--
-- The version row is the row the evidence already points at. A Consent Record
-- names a policy_versions id; a Staff Terms Acceptance names a terms_versions
-- id; migration 109's artifacts hang off both. An audit that has reached the
-- edition has reached these columns with it, in the same read, and there is
-- nothing to join and nothing that can be missing. A separate publish log is a
-- second table that can be truncated, that can lose a row to a failed second
-- statement, and that must be kept in step with a table it does not constrain —
-- three ways for the answer to "who did this" to drift away from the thing that
-- was done. The version row is append-only in practice and immutable in
-- principle (nothing in this codebase issues an UPDATE against either table),
-- so provenance written here is provenance that cannot drift.
--
-- THERE IS NO APPROVAL COLUMN, and there must never be one. Production holds
-- ONE platform_operators row with no roles and no second tier, so a two-person
-- rule would deadlock on every act; recruiting an approver would also hand them
-- payouts, reversals and invoicing backfill, because operator authority is one
-- boolean. What is irreversible is not the text but the RE-GATING, so the
-- substitute for a second pair of eyes is a night's delay on the acts that move
-- the gating floor (the service refuses a gating publish effective before
-- tomorrow) plus PROOF THAT THE OPERATOR WAS SHOWN THE CONSEQUENCE — which is
-- what `regated_headcount` is. It is the number that was ON THE BUTTON, stored
-- as it stood at the moment it was pressed, not a number recomputed later: an
-- audit asking "did they know?" is asking what the screen said, and a figure
-- recomputed against today's population answers a different question.
--
-- ALL SIX COLUMNS ARE NULLABLE AND WHOLE-OR-NOTHING. The two seeded editions
-- (migration 060's `0-placeholder`, migration 105's `1`) were published by a
-- migration and have no operator, no headcount and nothing to diff against.
-- Backfilling them with a plausible operator email would be the platform
-- asserting a fact about a person that never happened, so they keep NULL — the
-- honest answer — and the CHECK below makes "half a provenance" unstorable
-- rather than merely unlikely.
--
-- FORWARD-ONLY, like every migration here.

-- ---------------------------------------------------------------------------
-- The provenance columns, added identically to both tables.
--
-- Parallel and NOT generalized into a shared table, on ADR 0066's terms: the
-- two documents version independently, an edition of one must never re-gate the
-- other, and a shared provenance table would be one place where a Policy
-- publication and a Terms publication could be confused for each other.
-- ---------------------------------------------------------------------------

ALTER TABLE policy_versions
    -- Who pressed the button, by email, exactly as issued_by, annulled_by,
    -- reissued_by and house_designated_by are stored (ADR 0015). No foreign key
    -- to platform_operators: the trail must survive an operator's removal from
    -- the allowlist, and a reference would either block the delete or erase the
    -- history.
    ADD COLUMN published_by TEXT
        CHECK (published_by IS NULL OR published_by <> ''),
    -- When, by the server clock that performed the act. A TIMESTAMPTZ and not a
    -- DATE, unlike `effective_date` beside it, and the difference is the point:
    -- the effective date is a LEGAL FACT stated on the document, published to
    -- the day; this is the OPERATIONAL MOMENT the row was written, and the two
    -- are deliberately different values that an audit reads together — a
    -- correction published at 14:02 taking effect the same day, a gating edition
    -- published tonight taking effect tomorrow.
    ADD COLUMN published_at TIMESTAMPTZ,
    -- What the publication changed, in one line, counted in CELLS — one
    -- artifact in one language — because that is the unit the operator was shown
    -- the diff in (#562). It is a SUMMARY and never the diff itself: the diff is
    -- recomputable exactly, forever, from the two editions' own artifact rows,
    -- so storing it again would be a second copy that can disagree with the
    -- bytes. What is NOT recomputable is what the summary asserts — that this
    -- publication was understood to be this size.
    ADD COLUMN publish_diff_summary TEXT
        CHECK (publish_diff_summary IS NULL OR publish_diff_summary <> ''),
    -- The typed reason, on a CORRECTION only. A correction says "the words were
    -- wrong", and the one thing that cannot be recovered from the bytes is WHY
    -- somebody thought so. Bounded at 500 characters like the Operator
    -- Reversal's note and the reissue note: one sentence for a human, not a
    -- document. NULL when none was left, never an empty string.
    ADD COLUMN correction_reason TEXT
        CHECK (correction_reason IS NULL
           OR (correction_reason <> '' AND char_length(correction_reason) <= 500)),
    -- How many people this publication re-gated, AS THE BUTTON SAID. See the
    -- header: it is evidence about the screen, not a derived statistic.
    ADD COLUMN regated_headcount INTEGER
        CHECK (regated_headcount IS NULL OR regated_headcount >= 0);

ALTER TABLE policy_versions
    -- The trail travels whole or not at all. A row with an operator and no
    -- instant, or a headcount and no operator, is a row whose reader has to
    -- guess which half is missing and why.
    ADD CONSTRAINT policy_versions_publish_provenance_whole
        CHECK ((published_by IS NULL) = (published_at IS NULL)
           AND (published_by IS NULL) = (publish_diff_summary IS NULL)
           AND (published_by IS NULL) = (regated_headcount IS NULL)),
    -- A reason belongs to a correction and to nothing else. On a gating edition
    -- there is nothing to explain away: the edition IS the change, and its
    -- justification is the text itself.
    ADD CONSTRAINT policy_versions_correction_reason_only
        CHECK (correction_reason IS NULL OR revision > 0),
    -- ...and a correction published through the Legal Center must carry one.
    -- Grandfathered rows (published_by IS NULL) are exempt, which is the same
    -- exemption every column above grants them.
    ADD CONSTRAINT policy_versions_correction_reason_required
        CHECK (published_by IS NULL OR revision = 0 OR correction_reason IS NOT NULL),
    -- A CORRECTION RE-GATES NOBODY, made structural. This is the invariant the
    -- whole feature turns on — legal.GatingFloor skips corrections, so publishing
    -- one cannot move the floor and cannot move anybody into Outstanding — and a
    -- correction row claiming to have re-gated 1,058 people would be a lie the
    -- database was happy to store. It cannot be stored.
    ADD CONSTRAINT policy_versions_correction_regates_nobody
        CHECK (revision = 0 OR regated_headcount IS NULL OR regated_headcount = 0);

-- ---------------------------------------------------------------------------
-- terms_versions — the parallel table, the same six columns and the same four
-- constraints, for the same reasons.
-- ---------------------------------------------------------------------------

ALTER TABLE terms_versions
    ADD COLUMN published_by TEXT
        CHECK (published_by IS NULL OR published_by <> ''),
    ADD COLUMN published_at TIMESTAMPTZ,
    ADD COLUMN publish_diff_summary TEXT
        CHECK (publish_diff_summary IS NULL OR publish_diff_summary <> ''),
    ADD COLUMN correction_reason TEXT
        CHECK (correction_reason IS NULL
           OR (correction_reason <> '' AND char_length(correction_reason) <= 500)),
    ADD COLUMN regated_headcount INTEGER
        CHECK (regated_headcount IS NULL OR regated_headcount >= 0);

ALTER TABLE terms_versions
    ADD CONSTRAINT terms_versions_publish_provenance_whole
        CHECK ((published_by IS NULL) = (published_at IS NULL)
           AND (published_by IS NULL) = (publish_diff_summary IS NULL)
           AND (published_by IS NULL) = (regated_headcount IS NULL)),
    ADD CONSTRAINT terms_versions_correction_reason_only
        CHECK (correction_reason IS NULL OR revision > 0),
    ADD CONSTRAINT terms_versions_correction_reason_required
        CHECK (published_by IS NULL OR revision = 0 OR correction_reason IS NOT NULL),
    ADD CONSTRAINT terms_versions_correction_regates_nobody
        CHECK (revision = 0 OR regated_headcount IS NULL OR regated_headcount = 0);

-- ---------------------------------------------------------------------------
-- No index on any of these. Both tables hold a handful of rows for the lifetime
-- of the product — migration 060 says so, 110 declines to index them for the
-- same reason — and nothing filters on provenance: it is read WITH the edition,
-- by an audit that already has the row.
-- ---------------------------------------------------------------------------

COMMENT ON COLUMN policy_versions.regated_headcount IS
    'How many people this publication re-gated, as the confirm button said at the moment it was pressed (#563). Evidence that the consequence was shown, not a recomputed statistic; 0 on a correction, by constraint.';
COMMENT ON COLUMN terms_versions.regated_headcount IS
    'How many people this publication re-gated, as the confirm button said at the moment it was pressed (#563). Evidence that the consequence was shown, not a recomputed statistic; 0 on a correction, by constraint.';
