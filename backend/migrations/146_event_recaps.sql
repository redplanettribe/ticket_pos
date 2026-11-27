-- The record that an Event was recapped: one row per Event whose Event Recap
-- was sent, with the instant it went, or is being sent by a sweep holding it
-- (spec #884, #891).
--
-- THIS TABLE EXISTS ONLY TO SAY "ALREADY SENT" AND "BEING SENT". The Event
-- Recap goes once per Event, ever, and the sweep that sends it runs in a Cloud
-- Run instance that does not outlive the request, so "was this Event recapped"
-- is a question only the database can answer. A row with a sent_at makes its
-- Event never due again, whatever happens to the Event's dates afterwards: an
-- Event rescheduled later after its recap is not recapped a second time.
--
-- A SWEEP CLAIMS THE EVENT BEFORE ITS FIRST SEND, by writing the row with a
-- lease in claimed_until and no sent_at, and stamps sent_at once a mail was
-- accepted. Two sweeps that overlap, the scheduled run and one made by hand,
-- cannot both claim one Event, so neither can write to its readers a second
-- time. A sweep that dies holding a claim costs the Event its lease and not its
-- recap: once claimed_until has passed with no sent_at, the Event is due again.
-- The pattern is the Follow Digest's claim (ClaimDueDigest), on the one row the
-- Event has here.
--
-- ONE ROW PER EVENT, so the Event is the primary key and a second row cannot
-- exist. It is not a ledger of sends like assignment_reminders: there is no
-- "how many" or "when was the last" to ask, only whether.
--
-- WHAT IT DELIBERATELY DOES NOT RECORD is anything the mail said or who read
-- it: no figures, no recipients, no rendered body. The recap keeps no copy of
-- what it said, and no staff API reads this table.
--
-- ON DELETE CASCADE follows the Event: a deleted Event has nothing left to
-- recap or to remember.
--
-- Undone by hand with migrations/down/146_event_recaps.sql, which the runner
-- never applies.
CREATE TABLE event_recaps (
    event_id UUID PRIMARY KEY REFERENCES events (id) ON DELETE CASCADE,
    claimed_until TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ
);
