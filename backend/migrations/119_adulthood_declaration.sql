-- The Adulthood Declaration (#586, parent #584, ADR 0069): the answer to the
-- second, separate, un-premarked box beside the Terms — "I am eighteen or
-- older" — recorded wherever the Terms acceptance it travelled with is
-- recorded.
--
-- ONE MIGRATION, THREE COLUMNS, NO NEW TABLE. The declaration is a declared
-- fact about a person and not an acceptance of a document: it has no editions,
-- no lineage, no content hash and no satisfying set, so there is nothing for a
-- table of its own to hold. What IS editioned is the wording it was declared
-- under, and that already lives in `terms_version_artifacts` as the
-- `label-adulthood-declaration` Artifact, inside the edition's fingerprint —
-- which is why every column below sits beside a Terms answer that already names
-- the edition.
--
-- The three columns land together although only the first is written by this
-- ticket. The staff gate (#587) writes the second and the checkout (#588) the
-- third; they are one migration because they are one schema decision, and
-- splitting them would leave two later tickets each half-blocked on a DDL step
-- that has nothing to decide.
--
-- NULL MEANS THE ACT DID NOT ASK — never "No". A refusal is refused before any
-- capture and writes nothing at all (ADR 0069): the platform keeps no record of
-- anyone who says they are a minor, because such a row would be a permanent,
-- unverified assertion that a named individual is a child, on tables that are
-- never edited and never deleted, and it would go stale in the worst direction
-- as that person turned eighteen. So `false` is not a value any of these
-- columns is ever written with today, and the absence of a CHECK forbidding it
-- is migration 064's ruling restated: the refusal is about a MOMENT, not about
-- a row.
--
-- NO COLUMN ANYWHERE CARRIES AN AGE, A BIRTHDATE OR A THRESHOLD. Eighteen is a
-- number in prose inside the Artifact, in both languages — no setting, no
-- column, no per-Organization override, no per-Event rule. A birthdate is a far
-- more identifying datum, it invites a verification this platform cannot
-- perform, and it would need its own basis under the Privacy Policy. This is a
-- declaration and never a verification.
--
-- AND NOTHING IS ADDED TO `customers`. The current-state block there exists to
-- answer which boxes a person must still be SHOWN, which is Outstanding's
-- question; the declaration is welded to the Terms gate and nothing ever
-- decides independently whether to show it, so the answer is already in the
-- Terms pair beside it. Every pair in that block also names an edition of a
-- document, and this names none.

-- THE ATTENDEE CAPACITY, on the append-only evidence log (#586).
--
-- Nullable, and the null carries what the answer columns beside it carry: the
-- box was not shown on this surface. An account-settings toggle, an
-- unsubscribe, an operator-recorded withdrawal and a capture made under an
-- edition that does not carry the Artifact all say nothing about adulthood and
-- record nothing about it.
--
-- STORED, NEVER DERIVED, and deliberately redundant with the edition's Artifact
-- set: a refusal writes nothing, so every acceptance of an Artifact-carrying
-- edition necessarily ticked both boxes, and the answer could be recovered by
-- joining `terms_version_id` to that edition's slugs. It is written down anyway
-- because deriving it would make a fact about the past depend on how rows are
-- read in the present — a renamed slug, a mistyped slug in some later publish,
-- or a change to the slug-to-field mapping would silently rewrite what
-- thousands of people are recorded as having declared. That is the failure the
-- content hash exists to prevent, and `terms_acceptance` — already implied by
-- its paired `terms_version_id` — is the precedent this column follows.
ALTER TABLE consent_records ADD COLUMN adulthood_declaration BOOLEAN;

-- A declaration never travels alone. It is made in the same act that accepts
-- the Terms, so a record carrying one carries the acceptance and the edition it
-- was declared under; the reverse does not hold, because an edition that does
-- not publish the Artifact draws no box. An IMPLICATION and not migration 106's
-- equality, for exactly that asymmetry.
ALTER TABLE consent_records ADD CONSTRAINT consent_records_adulthood_declaration_check
    CHECK (adulthood_declaration IS NULL OR terms_acceptance IS NOT NULL);

-- THE ORGANIZER CAPACITY, on the Staff platform's own evidence (#587).
--
-- Nullable for the same reason and with the same meaning, over a table whose
-- every row IS an acceptance: null here reads "the edition this row names did
-- not carry the Artifact", which is true of every row written before an
-- operator publishes one. No paired CHECK, because there is no pair — the
-- acceptance and its edition are NOT NULL columns of the row itself, so the
-- implication above is already a fact about the table's shape.
--
-- Organizers declare it too, and that is ADR 0066's capacities read straight:
-- the organizer acceptance is the more contractual of the two, and capacity to
-- contract is precisely what is being declared. The Artifact is therefore
-- worded about BEING OF AGE rather than about buying, so that it reads true in
-- the capacity that is not buying anything.
ALTER TABLE staff_terms_acceptances ADD COLUMN adulthood_declaration BOOLEAN;

-- THE CHECKOUT'S HELD ANSWER (#588), beside the Terms hold migration 108 added
-- and under its discipline exactly.
--
-- `payments` carries the answers a held checkout will be COMMITTED with,
-- because the confirm leg arrives minutes later carrying a transaction id and
-- nothing else: a buyer fact not written down at begin-checkout is a buyer fact
-- lost by the time there is a sale to write it on. Held, not recorded — an
-- abandoned checkout evidences nothing.
ALTER TABLE payments ADD COLUMN consent_adulthood_declaration BOOLEAN NULL;

-- The paired CHECK, migration 108's rule over the new column: a declaration
-- with no Terms answer beside it holds no act. It points at
-- `consent_terms_acceptance` rather than at `consent_terms_version_id` because
-- those two are already pinned to each other by
-- `payments_terms_hold_check` — so naming either one names the edition too, and
-- naming the ANSWER says what this constraint is actually about: the
-- declaration rides the Terms box and is never held apart from it.
ALTER TABLE payments ADD CONSTRAINT payments_adulthood_hold_check
    CHECK (consent_adulthood_declaration IS NULL OR consent_terms_acceptance IS NOT NULL);

-- No index anywhere. Every column here is written with the row that holds it
-- and read back with that row — by primary key on `payments`, by a compliance
-- query over a small table on the two evidence logs — which is the verdict
-- migrations 106, 107 and 108 each reached about their own columns.
