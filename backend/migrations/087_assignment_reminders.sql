-- The ledger of Assignment Reminders: which Ticket Sale's buyer was written to
-- about the Tickets on it that still have nobody, and when (#362, parent #361,
-- ADR 0051).
--
-- THIS TABLE EXISTS ONLY TO SAY NO, on migration 079's terms exactly. Nothing
-- reads it to build a screen, nothing exports it and no Customer ever sees it.
-- Its whole purpose is the rationing in catalog.MayRemindAssignment — not
-- before 24 hours after the Sale, at most one reminder per Ticket Sale per 7
-- days, and at most two ever — and without persistence those rules cannot
-- exist: the job runs in a Cloud Run instance that does not outlive the
-- request, so "have we already told them" is a question only the database can
-- answer.
--
-- A LEDGER OF SENDS, NOT A COUNTER ON THE SALE, for 079's reason: the rationing
-- asks both "how many" and "when was the last", and two hand-maintained columns
-- eventually answer them inconsistently. One row per mail answers both from the
-- same fact, and keeps a job's writes out of the sales table every checkout and
-- every reversal already contends on.
--
-- ITS UNIT IS THE TICKET SALE, which is the unit 079 chose and 083 moved away
-- from — and the reason 083 gave is exactly the reason it is right here. 083
-- moved the Answer Reminder's ledger to the Ticket because ADR 0046 gave four
-- Tickets up to four Holders, and "one Sale is one inbox" stopped being true
-- for a mail addressed to whoever holds each Ticket. THIS mail is addressed to
-- the buyer and to nobody else: an unassigned Ticket has no Holder, and the
-- only person who can give it one is the person who bought it. One Sale IS one
-- inbox for this reader, a four-Ticket Sale with three unassigned produces ONE
-- mail carrying a count, and a per-Ticket ledger would let one buyer be mailed
-- three times in one sweep about three Tickets of one purchase.
--
-- IT IS NEITHER 083's LEDGER NOR 082's, AND MUST NOT BE CONFUSED WITH EITHER.
-- `answer_reminders` rations the Answer Reminder, per Ticket, to the Holder;
-- `ticket_assignment_mails` rations the Assignment mail, per Ticket, to the
-- named stranger. This one rations a mail to the BUYER, per Sale. Three mails,
-- three allowances, three tables; a sweep that read either of the others would
-- ration a buyer out of being told the choice exists because a friend of
-- theirs had been chased about a t-shirt size.
--
-- THIS SCHEMA SHIPS PAUSED, exactly as 079 did: the Cloud Scheduler job that
-- would write to it is created paused (assignment_reminder_enabled defaults
-- false), and the service refuses to find candidates while
-- TICKET_ASSIGNMENT_ENABLED is off. An empty table is the correct state of a
-- correct deployment until the Operator launches.
--
-- WHAT IT DELIBERATELY DOES NOT RECORD is anything about the message: no
-- recipient address, no subject, no body, no count of what was unassigned, no
-- provider message id. The address is on the Ticket Sale, where a Customer's
-- erasure request would already reach it; a copy here would be a second place
-- personal data lives, kept forever, for a job that knows where to read the
-- first (ADR 0045's last consequence). The tally at the time is derived and
-- never stored, exactly as the Customer Area derives it. All this table says is
-- "we wrote to the buyer of this Sale at this moment".
CREATE TABLE assignment_reminders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The Ticket Sale whose buyer was written to, and the unit of both caps.
    --
    -- ON DELETE CASCADE follows `ticket_sales`, which in practice deletes
    -- nothing: a Sale Reversal voids a sale and never removes it. The cascade is
    -- for the one path that does delete — an Event torn down while it was still
    -- a draft — where a reminder about a Sale that no longer exists would be a
    -- row about nothing.
    ticket_sale_id UUID NOT NULL REFERENCES ticket_sales (id) ON DELETE CASCADE,

    -- When the mail was accepted by the provider. WRITTEN AFTER THE SEND, never
    -- before: a row here is a claim that somebody's inbox has a message in it,
    -- and a row written first would ration a buyer out of a reminder they never
    -- received. The reverse failure — a send the recorded row missed — costs at
    -- most one duplicate on the next tick, reported by the sweep as
    -- `unrecorded`, which is the direction worth failing in.
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The only read this table has: "how many reminders has this Sale had, and when
-- was the last", asked once per candidate by every sweep. The Sale leads because
-- that is what is filtered on; sent_at follows so the MAX comes off the index
-- rather than out of a sort.
--
-- NOT UNIQUE on either column or on the pair, for 079's reason: a Sale is
-- expected to have two rows here over its life, and the cap and the cooldown
-- are decisions the job makes, not constraints a clock collision can express.
CREATE INDEX assignment_reminders_sale_sent_idx
    ON assignment_reminders (ticket_sale_id, sent_at DESC);
