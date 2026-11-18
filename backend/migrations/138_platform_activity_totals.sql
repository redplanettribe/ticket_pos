-- The daily close freezes activity into totals that name nobody (#815, spec
-- #808, ADR 0080).
--
-- Platform Trends states how many Organizations, and later how many
-- Customers, were active on each platform day. Those figures are frozen at
-- the close of the day and never recomputed on read: what they are counted
-- from (Ticket Sales by the instant recorded, and from the next tickets on
-- the activity markers, which are deleted after thirteen months and on
-- erasure) keeps moving or goes away, and a past day must read the same next
-- year.
--
-- One row per closed platform day, America/Guayaquil. A row is written once,
-- by the daily close, and never updated: a sale that turns up for a frozen
-- day changes nothing here.
--
--   day                        the platform day the row freezes.
--   active_organizations       the Organizations active that day.
--   active_organizations_7d    the different Organizations active across the
--                              7 days ending that day.
--   active_organizations_30d   the same across the 30 days ending that day.
--   frozen_at                  the instant the close wrote the row.
--
-- A day on which nobody was active has a row holding zeros. A day with no
-- row has not been frozen, which the read states as absent and never as
-- zero.
--
-- The three counts are whatever Active Organization meant on the day the row
-- froze: the sale half alone until Organization markers are recorded, both
-- halves from then on, in the same columns. The day that changes is to be
-- written down beside the markers, not inferred from these rows. A later
-- figure (Active Customers and its split) is added as NULLable columns, NULL
-- on every day frozen before that figure was recorded, so no frozen row is
-- ever rewritten to make room.
--
-- The table belongs to Platform Trends, the first it owns: the totals are
-- its figures and no other module's rows.
CREATE TABLE platform_activity_totals (
    day                      DATE PRIMARY KEY,
    active_organizations     INTEGER NOT NULL CHECK (active_organizations >= 0),
    active_organizations_7d  INTEGER NOT NULL CHECK (active_organizations_7d >= active_organizations),
    active_organizations_30d INTEGER NOT NULL CHECK (active_organizations_30d >= active_organizations_7d),
    frozen_at                TIMESTAMPTZ NOT NULL
);
