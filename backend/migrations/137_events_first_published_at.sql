-- An Event remembers when it was first published (#814, spec #808).
--
-- Platform Trends states Events Published: each Event counted once, on the
-- platform day it first became `published`. Until now an Event kept only its
-- status, so nothing said when that happened.
--
--   first_published_at  the instant of the Event's first transition to
--                       `published`. Set once, by the publish operation, and
--                       never cleared or moved afterwards, cancellation
--                       included. NULL for an Event never published.
--
-- THE BACKFILL IS A STAND-IN. An Event that is `published` when this runs
-- takes its creation instant, the nearest thing the platform kept; the dev
-- seeds insert `published` rows directly and are covered the same way. Every
-- other Event stays NULL: a `draft` because it was never published, and a
-- `cancelled` one because nobody can say it ever was, so it stays out of the
-- figure for good.
--
-- THE DAY THIS RUNS is the day exact recording of Events Published begins:
-- from it on first_published_at is the real instant, before it a creation
-- day. It is written down here, once, on the platform clock, rather than
-- inferred later from the data. The one-row table belongs to catalog, as
-- events does, and is deliberately absent from the integration harness's
-- TRUNCATE list: like a seeded edition, it is a fact of the deployment.
--
-- Every statement is safe to replay over a database it has already run on:
-- the backfill touches only `published` rows that have no instant yet, and
-- the recorded day is kept, never moved.
ALTER TABLE events ADD COLUMN IF NOT EXISTS first_published_at TIMESTAMPTZ;

UPDATE events
SET first_published_at = created_at
WHERE status = 'published' AND first_published_at IS NULL;

CREATE TABLE IF NOT EXISTS events_published_recording (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    began_on  DATE NOT NULL
);

INSERT INTO events_published_recording (began_on)
VALUES ((now() AT TIME ZONE 'America/Guayaquil')::date)
ON CONFLICT (singleton) DO NOTHING;
