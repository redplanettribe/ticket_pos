-- The Answer Reminder's ledger becomes PER TICKET (#328, parent #322, ADR 0046).
--
-- WHY THE UNIT MOVES. Migration 079 keyed this table on the Ticket Sale, with a
-- reason that was sound at the time and is now false: "a buyer holding four
-- unanswered Tickets is one person with one inbox". That held while the buyer
-- was the only person this mail could be addressed to. ADR 0046 gave four
-- Tickets up to four accepted Holders, and a per-Sale ledger cannot ration them:
-- the first Holder mailed would spend the whole Sale's allowance and the other
-- three would never be written to at all. The Sale is no longer one inbox, so it
-- is no longer the unit.
--
-- THE CAPS THEMSELVES ARE UNCHANGED — at most one mail per 7 days, at most two
-- ever, silence once the Event has started (catalog.MayRemind). What changes is
-- what they are counted against.
--
-- ONE MAIL STILL WRITES SEVERAL ROWS, and that is the property that keeps a
-- buyer's inbox from getting louder. The sweep groups every buyer-addressed
-- Ticket of one Sale into ONE message and then appends a row here for each
-- Ticket that message covered, so an ordinary four-Ticket sale with nothing
-- accepted still produces exactly the two mails it produced before — and now
-- eight rows instead of two. Rows here count TICKETS CHASED, never messages
-- sent, and nothing reads this table for a count of mail.
--
-- IT IS NOT #332'S LEDGER AND MUST NOT BE CONFUSED WITH IT.
-- `ticket_assignment_mails` (migration 082) rations the ASSIGNMENT mail — the
-- one telling a stranger that a friend bought them a ticket — and is spent by a
-- different route at a different moment. Two mails, two allowances, two tables;
-- a sweep that read or wrote 082's rows would ration a buyer out of assigning a
-- Ticket because somebody had been chased about a t-shirt size.
--
-- STILL EMPTY IN PRODUCTION, twice over: TICKET_QUESTIONS_ENABLED ships closed
-- and the Cloud Scheduler job that drives the sweep ships paused
-- (answer_reminder_enabled defaults false). Both are still true after this
-- migration, and #328 changes neither.

-- THE EXISTING ROWS ARE DISCARDED, and this is the only line here that destroys
-- anything. A row saying "this SALE was chased" cannot be translated into rows
-- saying which Tickets it covered — 079 deliberately recorded nothing about the
-- message, so there is no set of Tickets to recover — and fanning the row out
-- across every Ticket of the Sale would assert a fact the platform never held.
--
-- IT COSTS NOTHING, because there are no rows: the table is empty on every
-- deployment for the two reasons above, and an empty table is what its own
-- migration says is the correct state of a correct deployment. The DELETE is
-- here because a NOT NULL column cannot be added to a table that has rows, and
-- because a development database that HAS been used to exercise the sweep by
-- hand must not fail the migration or, worse, survive it carrying a per-Sale
-- allowance under a per-Ticket key.
DELETE FROM answer_reminders;

-- The Ticket the mail was about, and the unit of both caps.
--
-- ON DELETE CASCADE follows `tickets`, exactly as 082's does, and in practice
-- deletes nothing: a Sale Reversal voids a sale and never removes it. The
-- cascade is for the one path that does delete — an Event torn down while still
-- a draft — where a reminder about a Ticket that no longer exists would be a row
-- about nothing. It also inherits the reach the dropped `ticket_sale_id` had,
-- since a Ticket cascades from its Sale's lines.
--
-- IT REPLACES `ticket_sale_id` RATHER THAN JOINING IT. The Sale is reachable
-- from here in two hops (tickets -> ticket_sale_lines -> ticket_sales) and
-- nothing rations on it any more, so keeping both would be a second key that
-- could disagree with the first. One unit, one column.
ALTER TABLE answer_reminders ADD COLUMN ticket_id UUID NOT NULL REFERENCES tickets (id) ON DELETE CASCADE;

-- The old unit goes, and its index with it. Dropping the column drops
-- answer_reminders_sale_sent_idx automatically; it is named here so that a
-- reader looking for what became of 079's index finds the answer in a migration
-- rather than in a system catalogue.
ALTER TABLE answer_reminders DROP COLUMN ticket_sale_id;

-- "How many reminders has this Ticket had, and when was the last", asked once
-- per candidate by every sweep. 079's index with the key moved: the Ticket leads
-- because that is what is filtered on, and sent_at follows so the MAX comes off
-- the index rather than out of a sort.
--
-- STILL NOT UNIQUE on either column or on the pair, for 079's reason. A Ticket
-- is expected to have two rows here over its life, which is the whole point, and
-- uniqueness on (ticket, sent_at) would enforce nothing worth having — the cap
-- and the cooldown are decisions the job makes, not constraints a clock
-- collision can express. It matters slightly more now: ONE mail covering four
-- Tickets writes four rows sharing an instant, and they are four different
-- Tickets rather than one Ticket four times.
CREATE INDEX answer_reminders_ticket_sent_idx
    ON answer_reminders (ticket_id, sent_at DESC);
