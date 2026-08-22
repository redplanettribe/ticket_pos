-- The ledger of Answer Reminders: which Ticket Sale's buyer was written to
-- about Outstanding Answers, and when (#317, ADR 0044).
--
-- THIS TABLE EXISTS ONLY TO SAY NO. Nothing reads it to build a screen, nothing
-- exports it and no Customer ever sees it. Its whole purpose is the rationing in
-- catalog.MayRemind — at most one reminder per Ticket Sale per 7 days, and at
-- most two ever — and without persistence those rules cannot exist at all: the
-- job runs in a Cloud Run instance that does not outlive the request, so "have
-- we already told them" is a question only the database can answer.
--
-- A LEDGER OF SENDS, NOT A COUNTER ON THE SALE. The obvious cheaper design is
-- two columns on ticket_sales — reminders_sent and last_reminded_at — and it was
-- rejected because those two can disagree. The rationing asks both questions
-- ("how many" and "when was the last"), and a pair of hand-maintained columns
-- eventually answers them inconsistently: a partial write, a backfill, a
-- concurrent tick. One row per mail answers both from the same fact, and a
-- COUNT and a MAX cannot contradict each other. It also keeps a job's writes out
-- of the sales table, which every checkout and every reversal already contends
-- on.
--
-- THIS SCHEMA SHIPS PAUSED rather than dark, which is a different thing from
-- migrations 072-074 and worth stating. Those ship dark because
-- TICKET_QUESTIONS_ENABLED gates the collection that fills them. This one is
-- empty for that reason AND because the Cloud Scheduler job that would write to
-- it is created paused (answer_reminder_enabled defaults false). An empty table
-- here is therefore the correct state of a correct deployment twice over.
--
-- WHAT IT DELIBERATELY DOES NOT RECORD is anything about the message: no
-- recipient address, no subject, no body, no count of what was owed, no provider
-- message id. The address is on the Ticket Sale, which is where it belongs and
-- where a Customer's erasure request would already reach it; a copy here would
-- be a second place personal data lives, kept forever, for a job that already
-- knows where to read the first (ADR 0045's last consequence). What was owed at
-- the time is derived and never stored, exactly as the debt itself is (#313).
-- All this table says is "we wrote to the buyer of this Sale at this moment".
CREATE TABLE answer_reminders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The Ticket Sale whose buyer was written to. The unit of rationing is the
    -- SALE and not the Ticket or the question: a buyer holding four unanswered
    -- Tickets is one person with one inbox, and an Organization authoring a fifth
    -- question does not buy itself a fresh allowance to write to them.
    --
    -- ON DELETE CASCADE follows `ticket_sales`, which in practice deletes
    -- nothing: a Sale Reversal voids a sale and never removes it, and it keeps
    -- its Sale Confirmation reference precisely so it stays readable. The cascade
    -- is here for the one path that does delete — an Event torn down while it was
    -- still a draft — where a reminder about a Sale that no longer exists would
    -- be a row about nothing.
    ticket_sale_id UUID NOT NULL REFERENCES ticket_sales (id) ON DELETE CASCADE,

    -- When the mail was accepted by the provider. WRITTEN AFTER THE SEND, never
    -- before: a row here is a claim that somebody's inbox has a message in it,
    -- and a row written first would ration a buyer out of a reminder they never
    -- received. The reverse failure — a send the recorded row missed, because the
    -- process died between the two — costs at most one duplicate on the next
    -- tick, which is the direction worth failing in.
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The only read this table has: "how many reminders has this Sale had, and when
-- was the last", asked once per candidate by every sweep. The Sale leads because
-- that is what is filtered on; sent_at follows so the MAX comes off the index
-- rather than out of a sort.
--
-- NOT UNIQUE on either column or on the pair. A Ticket Sale is expected to have
-- two rows here over its life, which is the whole point, and uniqueness on
-- (sale, sent_at) would enforce nothing worth having — the cap and the cooldown
-- are decisions the job makes, not constraints a clock collision can express.
CREATE INDEX answer_reminders_sale_sent_idx
    ON answer_reminders (ticket_sale_id, sent_at DESC);
