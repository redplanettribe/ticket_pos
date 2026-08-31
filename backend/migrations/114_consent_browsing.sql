-- The acceptance browsers' two supports: an index for browsing the Customer
-- base by what it owes, and the normalisation the staff address key rests on
-- (#565, parent #556, ADR 0067).
--
-- Nothing here is a new table, a new column or a new state. "Who owes an
-- acceptance" has always been computable — it is `policy_version_id` not in the
-- satisfying set (#560) — and the reason it lived in psql rather than on a
-- screen is that nothing made it cheap and nothing made the staff half safe to
-- link to. This migration fixes both.

-- ---------------------------------------------------------------------------
-- 1. BROWSING THE CUSTOMER BASE BY WHAT IT OWES
-- ---------------------------------------------------------------------------
--
-- Today `customers` carries NO index on either version column: migration 062
-- said so and was right at the time — the pair is read by primary key with the
-- Customer row a sign-in already loads, and a per-edition report was "a rare
-- analytical question" (061). The acceptance browser makes it a screen, and a
-- screen is not rare.
--
-- (version_id, email) AND NOT (email, version_id), and the order is the whole
-- point. Every page this screen serves is one filter on the version column plus
-- a keyset seek on `email > $cursor ORDER BY email ASC LIMIT 51` — leading on
-- the version column makes that an index range scan whose rows arrive already
-- in email order, so Postgres reads 51 index entries and stops. Leading on
-- email would make it a full scan filtered afterwards.
--
-- NULLS ARE IN THE INDEX, and that is load-bearing rather than incidental. A
-- btree indexes NULL, so `policy_version_id IS NULL` — NEVER SEEN, which on the
-- production copy is 511 of 1,569 Customers, about a third — is served by the
-- same index as the other three states. A partial index (WHERE ... IS NOT NULL)
-- would have made the largest bucket on the screen the one that scans the table.
--
-- Two indexes and not one composite over both columns: the screen filters by
-- DOCUMENT (#565), so the Policy question and the Terms question are asked
-- separately and never together. An index over both would serve neither.
--
-- NOT UNIQUE, obviously: an edition is held by as many people as accepted it.
CREATE INDEX customers_policy_version_email_idx ON customers (policy_version_id, email);
CREATE INDEX customers_terms_version_email_idx ON customers (terms_version_id, email);

-- `staff_terms_acceptances` needs nothing: its UNIQUE index already leads on
-- `email` (migration 107), which is the order the staff browser pages in, and
-- the staff population is small enough that its own scan is free.

-- ---------------------------------------------------------------------------
-- 2. EMAIL NORMALISATION, BECAUSE A DIGEST IS DERIVED FROM AN ADDRESS
-- ---------------------------------------------------------------------------
--
-- The staff browser links to a per-subject screen keyed on a DIGEST of the
-- address (#565): 32 hex chars of HMAC-SHA256 over "staff:" + the normalised
-- email, under a purpose-derived subkey of CONFIRMATION_LINK_SECRET (ADR 0046).
-- A staff person has no id — the person key of the Staff platform is the email
-- a session names (migrations 067, 069, 107) — so the digest is the only way to
-- name one in a URL without putting a data subject's address in it.
--
-- THAT MAKES `lower(btrim(email))` A CORRECTNESS PROPERTY AND NOT A TIDINESS
-- ONE. The digest is derived from platform.NormalizeEmail(email), so a row
-- holding `Ana@Example.com` would be listed under the digest of
-- `ana@example.com` and reached by it — which is right — while `members` and
-- `platform_operators` would still contain a second spelling that no query on
-- this screen would ever join back to. Two spellings of one address are two
-- people to a screen keyed on a fold of one of them.
--
-- NORMALISATION IS ENFORCED NOWHERE IN POSTGRES TODAY. Every Go entry path
-- normalises (platform.NormalizeEmail), and that has been enough while the
-- address was only ever compared against another normalised address. But
-- `platform_operators` rows are inserted BY HAND IN PSQL in production — 024
-- says so and says there will never be a UI for it — so the one table whose
-- rows bypass every Go path is the one that grants platform-wide authority.
-- A CHECK is where a hand-typed row is told.
--
-- lower(btrim(...)) IS platform.NormalizeEmailSQL, the SQL shadow of
-- platform.NormalizeEmail, kept as one expression for #398's reasons. It is
-- written out here because a migration cannot call Go; if that fold ever
-- changes, TestNormalizeEmailSQLFoldsTheSameWayAsNormalizeEmail is what says so.
--
-- VERIFIED CLEAN ON THE PRODUCTION COPY before writing this: 31 members, 1
-- operator, 14 staff locales, 1 staff acceptance — zero uppercase, zero
-- untrimmed. The CHECKs are therefore additions that reject nothing that
-- exists; the DO block below refuses the migration rather than let a
-- constraint violation be the first anybody hears of a bad row.
DO $normalized$
DECLARE
    offenders INT;
BEGIN
    SELECT
        (SELECT count(*) FROM platform_operators WHERE email <> lower(btrim(email)))
      + (SELECT count(*) FROM members WHERE email <> lower(btrim(email)))
      + (SELECT count(*) FROM staff_locales WHERE email <> lower(btrim(email)))
      + (SELECT count(*) FROM staff_terms_acceptances WHERE email <> lower(btrim(email)))
    INTO offenders;

    IF offenders > 0 THEN
        RAISE EXCEPTION
            'migration 114: % staff email row(s) are not normalised; normalise them before adding the CHECK, and do not weaken the constraint',
            offenders;
    END IF;
END
$normalized$;

ALTER TABLE platform_operators ADD CONSTRAINT platform_operators_email_normalized_check
    CHECK (email = lower(btrim(email)));
ALTER TABLE members ADD CONSTRAINT members_email_normalized_check
    CHECK (email = lower(btrim(email)));
ALTER TABLE staff_locales ADD CONSTRAINT staff_locales_email_normalized_check
    CHECK (email = lower(btrim(email)));
ALTER TABLE staff_terms_acceptances ADD CONSTRAINT staff_terms_acceptances_email_normalized_check
    CHECK (email = lower(btrim(email)));

-- `customers` IS DELIBERATELY EXCLUDED, and this is a decision rather than an
-- omission (#565). It is UUID-keyed, so no screen ever needs a digest of a
-- Customer's address and none is minted; it is by far the largest of the five
-- tables, so the CHECK would be the expensive one; and its UNIQUE(email) plus
-- the normalisation every entry path performs (migration 016) already carry the
-- property the staff tables lacked. Adding it here would buy nothing this
-- feature needs and would be the only whole-table validation in the file.
