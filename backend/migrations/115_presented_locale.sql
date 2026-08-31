-- WHICH LANGUAGE THE PERSON WAS ACTUALLY SHOWN, on the two tables that record
-- an act of consent (#567, parent #556, ADR 0067).
--
-- The platform already records WHICH EDITION was accepted — the fingerprint
-- covers every published language of it, so the hash alone cannot say which one
-- was on screen. That is the question an operator is eventually asked: not "was
-- this text in force" but "was this notice comprehensible to this person". One
-- column answers it, and nothing derived from a request header can: the page's
-- locale is what the person asked for, and the platform may have served
-- something else.
--
-- SO IT HOLDS THE LOCALE OF THE ARTIFACT RENDERED, NEVER OF THE PAGE IT WAS
-- RENDERED ON. The staff terms gate falls back to the prevailing text when the
-- edition does not publish the language the login page is in
-- (identity/service.acceptanceLabel), and a row that copied the request's
-- locale there would assert that somebody read English when they were shown
-- Spanish. The writer is therefore the renderer, which is why acceptanceLabel
-- now returns the locale it used alongside the string, and why this is a
-- per-caller field on consent.Evidence rather than something
-- EvidenceFromRequest could ever fill in.
--
-- NULLABLE, AND NULL IS NOT A GAP. Six of the capture channels present no fresh
-- document at all and are permanently NULL: the Customer Area toggles,
-- unsubscribe, digest, `operator_request` and `passcode_withdrawal` show no
-- notice, and `email_confirmation` confirms an earlier act rather than showing
-- new text. NULL there is the truthful answer — nothing was rendered — and it
-- is the same NULL every row written before this migration carries, which is
-- why THERE IS NO BACKFILL: what language a past capture displayed is not
-- recoverable from anything stored, and inventing it would put a guess into an
-- evidence log.
--
-- THERE IS DELIBERATELY NO CHECK TYING THIS TO THE PRESENCE OF AN ACCEPTANCE,
-- unlike the terms answer/edition pair beside it (migration 106). A sign-in row
-- carrying nothing but a marketing answer was still rendered on a page
-- displaying the Short Notice, and a constraint saying otherwise would refuse
-- the truth. The only CHECK here is over the VALUE, matching every other locale
-- column on this platform (migrations 051, 059, 069): the platform serves two
-- languages and a third would be a code change landing beside its own
-- migration.
--
-- It is NOT added to `customers`: that row is derived state — what is true now —
-- and the language somebody was shown is a fact about an ACT. Nor to the
-- payment hold (migration 108), because the commit leg appends the real
-- Consent Record and `payments.locale` (migration 059) is already on the same
-- INSERT that reads it.
ALTER TABLE consent_records ADD COLUMN presented_locale TEXT NULL
    CHECK (presented_locale IN ('en', 'es'));

-- The Staff platform's one consent gate (migration 107), which owes the same
-- answer for the same reason — and owes it more sharply, because the staff
-- login page is the one surface where the fallback described above actually
-- fires today.
ALTER TABLE staff_terms_acceptances ADD COLUMN presented_locale TEXT NULL
    CHECK (presented_locale IN ('en', 'es'));

-- And how that answer survives the two requests the staff gate is made of.
--
-- The label is rendered when the terms step is ISSUED and the acceptance row is
-- written when the step is FINISHED, minutes later, by a request that carries a
-- token and a ticked box. The locale of the text that was on screen is known
-- only to the first of those, exactly as the edition is — so it is pinned here,
-- beside `terms_version_id`, by the same argument that put that column here:
-- evidence must name what was shown, and only the surface that showed it knows.
--
-- It is NOT a second `presented_locale`. A pending row is a held sign-in and
-- not evidence (see this table's own note in migration 107): it is deleted when
-- it is spent, it is deleted when it expires, and nothing reads it to answer a
-- question about the past. It is named for what it holds — the locale of the
-- acceptance label that was served — and it is nullable because an edition that
-- publishes no label at all fails the gate before it ever gets here.
--
-- Nothing analogous is added to `pending_consents` (migration 063), and that
-- asymmetry is deliberate rather than an oversight. That table declined to
-- carry a locale from the PROVING to the TICKING; this column wants the one at
-- the ticking, and on the customer side the ticking page is the page that
-- rendered the Short Notice and can simply say so. The staff side has no such
-- page: the label is served by the API, not fetched by the form.
ALTER TABLE pending_staff_terms ADD COLUMN label_locale TEXT NULL
    CHECK (label_locale IN ('en', 'es'));
