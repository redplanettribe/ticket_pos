-- THE OPERATOR'S OWN READS OF PEOPLE'S DATA (#569, parent #556, ADR 0067).
--
-- Every other act on this platform is attributed by hanging a `recorded_by`
-- column off the domain row the act produced: a Payout names who recorded it, a
-- Consent Record names the operator who wrote a withdrawal down, a version row
-- names who published it. That pattern works because every one of those acts
-- LEAVES A ROW. A READ LEAVES NOTHING — the whole of what happened is that
-- somebody looked — so there is no domain row to hang attribution off, and the
-- act has to become a row of its own. That is the one reason this table exists,
-- and it is the reason it is the ONLY new table in the Legal Center that is not
-- about a document.
--
-- WHAT IT IS FOR. The platform holds consent evidence about people who are
-- entitled to ask what was done with it. "Nobody looked at your record except
-- the operator who answered your request, on this date" is an answer the
-- platform can only give if it wrote the looking down. So the four ways
-- somebody's data can be touched from the Legal Center each write one row here:
-- browsing a population, reading one person's record, exporting their Evidence
-- Pack, and READING THIS LOG — because a touch of people's data must be
-- recorded however it is reached, and a log that exempted its own reader would
-- be a hole shaped exactly like the person most likely to use it.
--
-- WHAT IT IS NOT FOR, which is the harder half:
--
--   - IT IS NOT A SECOND COPY OF THE ROSTER. A `list_read` records THE QUESTION
--     — which population, which document, which filter, and how many rows came
--     back — and never the answer. A log that stored who was on the page would
--     be an unbounded, unindexed, unerasable second copy of the very list it
--     exists to audit, growing by fifty names every time an operator clicked
--     "load more".
--   - IT IS NOT A PLACE EMAIL ADDRESSES ACCUMULATE. `search_term` is a BOOLEAN
--     and not the term: an operator who types an address into the search box to
--     find one person must not thereby write that address into a log. Whether a
--     search narrowed the page is the audit-relevant fact ("this was a lookup,
--     not a sweep"); the fragment itself is not.
--   - IT IS NOT AN ACTIVITY FEED. Publishing, correcting, scheduling and
--     cancelling an edition write NO row here: they are provenance columns on
--     the version row (migrations 112 and 113), so provenance can never drift
--     from the edition it describes. A withdrawal writes no row either — the
--     `consent_records` row it produces already evidences it, with the operator,
--     the reference and the prior values, and is a strictly better record. And a
--     preview writes no row: #562 demoted it to a platform.Logger line. Every
--     row in this table is therefore a TOUCH OF SOMEBODY'S DATA and nothing
--     else, which is what makes the table readable at all.
--
-- RETENTION IS UNBOUNDED. There is no purge, no retention window, no TTL and no
-- archival job, here or in the code. Evidence that ages out is evidence the
-- platform cannot produce on the day it is asked for, and this table grows by
-- one row per screen an operator opens — a rate measured in hundreds per year on
-- a platform with one operator. There is also NO EXPORT: it is read where it
-- lives, on one screen, because an audit log that can be downloaded is an audit
-- log that can be circulated.

CREATE TABLE IF NOT EXISTS consent_access_log (
    -- BIGSERIAL rather than the UUID every other table on this platform uses,
    -- and the reason is the read: this is an append-only log paged newest-first
    -- on (occurred_at DESC, id DESC), and a monotonic id is what makes the
    -- tiebreak within one timestamp both stable and meaningful. It is also the
    -- one table here whose rows are written by the platform about itself, so
    -- there is no id to correlate with anything outside it.
    id BIGSERIAL PRIMARY KEY,

    -- The four ways somebody's data is touched. An OPEN CHECK in the house
    -- style: a fifth act is a code change landing beside its own migration, not
    -- a string somebody can invent at runtime.
    act TEXT NOT NULL CHECK (act IN ('list_read', 'subject_read', 'evidence_export', 'audit_read')),

    -- WHO LOOKED, taken from the Staff Session and never from a body.
    --
    -- PLAIN TEXT WITH NO FOREIGN KEY, following migration 067's argument
    -- verbatim rather than restating it: revoking operator authority is a
    -- DELETE from `platform_operators`, and an FK would either block that
    -- revocation — leaving somebody an operator because they once read a
    -- record — or, with a cascade, erase the attribution this table exists to
    -- preserve at the exact moment somebody's access is being withdrawn. The
    -- address is the evidence, and it must outlive the authority.
    actor_email TEXT NOT NULL CHECK (actor_email <> ''),

    -- THE SERVER'S CLOCK, defaulted rather than passed in. Nothing above this
    -- row supplies a timestamp, so no caller and no test can name one.
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- ------------------------------------------------------------------
    -- THE QUESTION A LIST READ ASKED. Present on `list_read` and `audit_read`,
    -- absent on the two acts that name a person.
    -- ------------------------------------------------------------------

    -- Which population was browsed. NULL on `audit_read`, which browses the
    -- platform's own acts and no population at all.
    population TEXT CHECK (population IN ('customer', 'staff')),
    -- Which document's gate the page was about. NULL on `audit_read` for the
    -- same reason.
    document TEXT CHECK (document IN ('policy', 'terms')),
    -- The standing filter on a browser (`outstanding`, `never_seen`, `current`,
    -- `former`), or the act filter on the audit reader. FREE TEXT rather than a
    -- CHECK, deliberately: it is a record of what was asked, and a constraint
    -- here would have to be widened every time either screen gained a filter
    -- value — turning an append-only log into something a migration edits.
    status_filter TEXT,
    -- WHETHER A SEARCH NARROWED THE PAGE, AND NEVER WHAT WAS SEARCHED FOR.
    -- A boolean, so that looking one person up cannot deposit their address in
    -- an audit log — which would be this feature causing precisely the harm it
    -- exists to detect.
    search_term BOOLEAN,
    -- How many rows the page returned. The other half of "was this a lookup or
    -- a sweep", and the one number that makes a `list_read` row worth reading.
    result_count INTEGER CHECK (result_count >= 0),

    -- ------------------------------------------------------------------
    -- WHO WAS LOOKED AT. Present on `subject_read` and `evidence_export`,
    -- absent on the two acts that ask a question about a population.
    -- ------------------------------------------------------------------

    -- The Customer this act was about, where it was about a Customer.
    --
    -- ON DELETE RESTRICT, matching `consent_records` (migration 061) and every
    -- other reference to a data subject on this platform. DELETING A CUSTOMER
    -- WITH LOG ROWS FAILS LOUDLY, which is the intended behaviour and not an
    -- inconvenience: an erasure request escalates to counsel, and a cascade
    -- here would let a routine DELETE quietly destroy the record of who read
    -- somebody's file — the one record that answers the question an erasure
    -- request is usually asking.
    subject_customer_id UUID REFERENCES customers(id) ON DELETE RESTRICT,
    -- The subject BY NAME, because on these two acts the subject IS the act:
    -- "somebody's record was opened" with the somebody left out records nothing
    -- worth keeping.
    --
    -- A STAFF SUBJECT IS RECORDED HERE AS A PLAIN ADDRESS AND NEVER AS THEIR
    -- STAFF DIGEST. The digest is a URL key derived from the deployment's link
    -- secret (ADR 0046), and it is written to no row anywhere for exactly this
    -- reason: a key rotation would turn every historic row into an identifier
    -- naming nobody, orphaning a log whose entire purpose is to stay meaningful
    -- for years. The digest is resolved to the address before the row is
    -- written.
    subject_email TEXT CHECK (subject_email <> ''),

    -- ------------------------------------------------------------------
    -- WHAT WAS HANDED OVER. `evidence_export` only.
    -- ------------------------------------------------------------------

    -- The SHA-256 of the pack that was generated, lowercase hex — the same
    -- value migration 118 stores on the artifact row and the same value the
    -- filename is keyed on. Together they answer both halves of a handover:
    -- 118 says a file with these bytes was produced and what it covered, this
    -- says who produced it and about whom. The actor lives here and not there
    -- because attribution belongs to the ACT; duplicating it would be two facts
    -- that can disagree.
    pack_sha256 TEXT CHECK (pack_sha256 ~ '^[0-9a-f]{64}$'),

    -- THE ROW CANNOT BE HALF-WRITTEN.
    --
    -- Every column above is nullable on its own, because each applies to some
    -- acts and not others, and a table of eleven independently-nullable columns
    -- is a table in which a `list_read` with no count, or an
    -- `evidence_export` with no subject, is a perfectly legal row. Those rows
    -- would be worse than absent: an audit log with a hole in it reads as
    -- evidence right up until somebody relies on it.
    --
    -- So the act decides the shape of its own row, and the shape is enforced
    -- HERE rather than in Go — the house style (migrations 099, 102, 113) —
    -- because a constraint in the database holds for a hand-typed psql INSERT,
    -- a future caller and a backfill alike, and this is the one table whose
    -- credibility is its whole purpose.
    --
    -- ELSE FALSE and not a bare CASE: an unrecognised act cannot slip through
    -- as NULL (which a CHECK reads as satisfied) if the enum above is ever
    -- widened without this constraint being widened with it.
    CONSTRAINT consent_access_log_act_whole CHECK (
        CASE act
            -- A QUESTION ABOUT A POPULATION, WHOLE: which population, which
            -- document, which filter, whether it was searched and how many rows
            -- came back. And NO SUBJECT — a list read is about nobody in
            -- particular, and a subject on one of these rows would mean the
            -- roster had started leaking in one name at a time.
            WHEN 'list_read' THEN
                population IS NOT NULL
                AND document IS NOT NULL
                AND status_filter IS NOT NULL
                AND search_term IS NOT NULL
                AND result_count IS NOT NULL
                AND subject_customer_id IS NULL
                AND subject_email IS NULL
                AND pack_sha256 IS NULL
            -- READING THIS LOG. It browses no population and no document, so
            -- both are NULL; what it does carry is the same audit-relevant pair
            -- every list read does — was it narrowed, and how much came back.
            -- `status_filter` holds the act filter and is NULL when the reader
            -- asked for everything, so absent means "unfiltered" rather than
            -- "not recorded".
            WHEN 'audit_read' THEN
                population IS NULL
                AND document IS NULL
                AND search_term IS NOT NULL
                AND result_count IS NOT NULL
                AND subject_customer_id IS NULL
                AND subject_email IS NULL
                AND pack_sha256 IS NULL
            -- ONE PERSON'S RECORD WAS OPENED. The subject is named and the
            -- filter columns are forbidden: a subject read asked no question
            -- about a population, and a count on such a row would be a number
            -- with nothing to count.
            WHEN 'subject_read' THEN
                subject_email IS NOT NULL
                AND population IS NULL
                AND document IS NULL
                AND status_filter IS NULL
                AND search_term IS NULL
                AND result_count IS NULL
                AND pack_sha256 IS NULL
            -- A FILE ABOUT ONE PERSON LEFT THE PLATFORM. Subject and
            -- fingerprint together, both required: an export row without the
            -- hash could not be matched to the file somebody is holding, which
            -- is the only thing an export row is ever asked to do.
            WHEN 'evidence_export' THEN
                subject_email IS NOT NULL
                AND pack_sha256 IS NOT NULL
                AND population IS NULL
                AND document IS NULL
                AND status_filter IS NULL
                AND search_term IS NULL
                AND result_count IS NULL
            ELSE FALSE
        END
    )
);

-- THE READER'S ONE ORDER: newest first, always. There is no other way to read
-- this table from the platform, so there is one index for it, and the id is in
-- it because it is the keyset tiebreak — two acts inside one clock tick must
-- page deterministically or the walk repeats a row.
CREATE INDEX IF NOT EXISTS idx_consent_access_log_occurred_at
    ON consent_access_log (occurred_at DESC, id DESC);

-- "WHAT DID THIS OPERATOR LOOK AT" — the question asked when somebody's access
-- is being reviewed, and the reason the reader filters by actor at all.
CREATE INDEX IF NOT EXISTS idx_consent_access_log_actor
    ON consent_access_log (actor_email, occurred_at DESC, id DESC);

-- AND DELIBERATELY NO INDEX ON `subject_email` OR `subject_customer_id`.
--
-- It is not an omission and it is not a performance judgement: the reader
-- offers no subject filter, so the platform has no query that would use one.
-- An index here would be the first half of building the thing this design
-- refuses — a second way to look people up, keyed on the log of people being
-- looked up — and the cheapest way to stop that being added by accident is for
-- the query to be slow and the absence to be written down.
