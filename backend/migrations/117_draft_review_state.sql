-- What the operator has actually LOOKED AT before publishing (#562, spec #556).
--
-- Migration 111 gave each legal document one mutable draft. This one records two
-- facts about that draft which #563 turns into publish preconditions: every
-- artifact has been PREVIEWED as a reader will see it, and the DIFF against the
-- current edition has been SEEN. A publication is the platform changing the
-- contract everybody is held to, and "I only ever saw it as a textarea" is not a
-- state the button should be reachable from.
--
-- REVIEW STATE IS NOT EVIDENCE, exactly as 111's tables are not evidence. No row
-- here is hashed into a fingerprint, pointed at by an acceptance, or shown to a
-- reader; the digests below exist only to notice that the text has changed since
-- somebody looked at it. They are deliberately NOT `legal.ContentHash` values —
-- that function computes the published preimage (#541), and reusing it here
-- would put an evidence fingerprint on a draft, which 111 is explicit about not
-- doing. The service computes its own digest and says so.
--
-- WHY DIGESTS RATHER THAN BOOLEANS. A flag would be set by one look and would
-- then survive every later edit: preview the policy, rewrite a paragraph, and
-- the button stays lit over text nobody has seen. Storing WHAT WAS LOOKED AT
-- means the review state expires by itself the moment the words move, with no
-- invalidation pass to forget to run. Saving a draft unchanged leaves it intact,
-- which is the behaviour an operator expects from pressing Save twice.
--
-- FORWARD-ONLY, like every migration here.

-- The diff, remembered by what it was a diff OF.
--
-- BOTH SIDES, because a diff is a statement about a PAIR. Re-reading the draft
-- after somebody else published underneath it would otherwise leave the operator
-- holding a diff against an edition that is no longer current — the exact
-- situation `base_is_current` exists to surface — and publishing on the strength
-- of it would ship a comparison nobody made.
ALTER TABLE legal_drafts
    -- The digest of the draft's artifact rows at the moment the diff was shown.
    -- NULL means no diff has been seen; a value that no longer matches the draft
    -- means one was seen and has since gone stale.
    ADD COLUMN diff_seen_draft_digest TEXT,
    -- The published version the diff was taken against. Untyped as UUID for the
    -- same reason `base_version_id` is not a foreign key (migration 111): it
    -- names a row in one of two tables depending on `document`.
    ADD COLUMN diff_seen_version_id UUID,
    ADD COLUMN diff_seen_at TIMESTAMPTZ,
    ADD COLUMN diff_seen_by TEXT;

-- One row per artifact cell an operator has previewed.
--
-- THE UNIT IS THE CELL — (artifact, language) — and not the artifact, because a
-- preview is a rendering of ONE body of text. Having read the English policy
-- says nothing about whether the Spanish one renders as prose or as one
-- 280-line paragraph, and the whole point of the rule is that nobody publishes
-- words they have only seen in a textarea. The key is exactly
-- `legal_draft_artifacts`' key, so the two line up row for row.
--
-- ON DELETE CASCADE from the draft: discarding a draft discards what was
-- reviewed about it, because there is nothing left to have reviewed.
CREATE TABLE legal_draft_previews (
    document TEXT NOT NULL REFERENCES legal_drafts (document) ON DELETE CASCADE,
    locale TEXT NOT NULL,
    slug TEXT NOT NULL,
    -- The digest of the body that was on screen. A preview of text that has
    -- since been edited is not a preview of the draft, and the service compares
    -- rather than trusts. This is why saving the draft does NOT delete these
    -- rows: an unchanged cell is still previewed, and a changed one expires on
    -- its own.
    body_digest TEXT NOT NULL,
    previewed_by TEXT NOT NULL,
    previewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (document, locale, slug)
);

COMMENT ON TABLE legal_draft_previews IS
    'Which draft cells an operator has seen rendered, keyed by what they saw (#562). Review state, never evidence; a preview is also a platform.Logger line and never a consent_access_log row (#545).';
