-- The record that an Event was recapped: one row per Event whose Event Recap
-- was sent, with the instant it went (spec #884, #891).
--
-- THIS TABLE EXISTS ONLY TO SAY "ALREADY SENT". The Event Recap goes once per
-- Event, ever, and the sweep that sends it runs in a Cloud Run instance that
-- does not outlive the request, so "was this Event recapped" is a question only
-- the database can answer. A row makes its Event never due again, whatever
-- happens to the Event's dates afterwards: an Event rescheduled later after its
-- recap is not recapped a second time.
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
    sent_at TIMESTAMPTZ NOT NULL
);
