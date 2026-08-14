-- The Staff Locale: the language a person who signs in to staff is written to
-- and written for (#284, parent #281).
--
-- A TABLE OF ITS OWN, KEYED ON EMAIL, AND NOT A COLUMN ON `members`. That is the
-- whole decision this migration records, and it is worth stating before someone
-- notices the join and folds it back in.
--
-- `members` is UNIQUE (organization_id, email): one human who works for two
-- Organizations has two rows there. A locale column on it would give that human
-- two languages, and the staff app would change language when they switched
-- Organization — a working language that is a property of whichever Organization
-- they happen to be looking at. And a Platform Operator need not be a Member of
-- anything at all (ADR 0015), so a column on `members` would leave the people who
-- read the Operator Dashboard with nowhere to put one.
--
-- Email is not an arbitrary key here, it is THE key staff identity already uses:
-- `sessions.email`, `otp_challenges.email` and `platform_operators.email` are all
-- keyed on it, and a Staff Session's Active Member is a pointer hung off the
-- side. So this table says what those already say — a person is an address — and
-- one address holds one language, across every Organization and every device.
-- It is also what lets a Payout Request notice be localized at all: those are
-- addressed to a recorded email string deliberately tied to no Member id, and an
-- address is exactly what this table can be asked about.
--
-- ABSENT BY DEFAULT, AND NO BACKFILL. There is no row per Member, no row per
-- session, and existing Members get nothing. Absence is not the same fact as
-- 'en' and this table is careful to keep them apart, for the reason migration
-- 059 gave for leaving the Sale Locale NULL: writing 'en' for somebody asserts
-- that they CHOSE English. A person invited last month and never signed in, and
-- an email address that only ever appeared on a Payout Profile, have stated no
-- language whatsoever. They are read in English because English is the floor
-- every reader falls to, not because they picked it — and the day they sign in
-- from a Spanish browser, an absent row is what lets their choice be recorded
-- instead of found already taken.
--
-- The consequence is the sign-in rule this table exists to serve: a sign-in that
-- names a detected language INSERTs when there is no row and does nothing when
-- there is (ON CONFLICT DO NOTHING). Detection is weak evidence — a browser
-- header and a login page the person did not object to — and it must never
-- overwrite the strong evidence of somebody having used the switcher.
CREATE TABLE staff_locales (
    -- Normalised exactly as `sessions.email` is, by the same platform helper on
    -- the way in. The primary key IS the person: presenting an address is the
    -- whole lookup, and one address cannot hold two languages.
    --
    -- NO FOREIGN KEY, deliberately. There is no table of staff people to point
    -- at — `members` is per-Organization, `sessions` is per-sign-in and
    -- disposable, and the operator allowlist holds only operators. A constraint
    -- against any of them would delete a person's language when they left an
    -- Organization or when their session expired.
    email TEXT PRIMARY KEY,
    -- The two languages this platform is written in, CHECKed as
    -- `customers.mail_locale` and `ticket_sales.locale` are. NOT NULL because a
    -- row here means a language was stated: absence of a preference is absence
    -- of the row, and a NULL would be a third way to say the same thing.
    locale TEXT NOT NULL CHECK (locale IN ('en', 'es')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- When the language was last stated. Not read by anything today; it is the
    -- difference between "detected at first sign-in months ago" and "chosen
    -- deliberately this morning", which is the first question anyone will ask of
    -- a row that looks wrong.
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- No further index. Every read is a primary-key lookup by one address, while
-- composing that person's own page or that person's own mail, and nothing ever
-- selects on the language.
