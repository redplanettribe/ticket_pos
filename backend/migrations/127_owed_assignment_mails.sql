-- The Assignment mails a Named Tickets checkout owes, waiting for the swept
-- sender to send them (#670, parent #665, ADR 0076).
--
-- WHY A QUEUE AND NOT AN INLINE SEND. The commit that records a Ticket Sale
-- writes every Ticket Assignment the buyer named at checkout in its own
-- transaction, and a transaction cannot send mail: a mail cannot be rolled
-- back, and nine of them in one burst is exactly what tripped the provider's
-- rate limit on the Assignment Reminder's first run. So the commit writes one
-- row here per Ticket it `assigned` to somebody other than the buyer, and a
-- paced, swept sender (#671) sends them afterwards and retries what fails.
-- An own-address Ticket is `accepted` at once and owes nothing, so it has no
-- row; neither does an assignment made after the sale, which keeps sending
-- inline with its rationing, as before.
--
-- A ROW IS "A MAIL IS OWED", AND THE ROW GOING IS "IT IS NOT OWED ANY MORE".
-- The sweep deletes it when the provider accepts the send - writing the
-- ledger row in `ticket_assignment_mails` (migration 082) at that moment, on
-- that table's own "written after the send" rule - and deletes it unsent when
-- the mail has become pointless (see assigned_at below). A failed send leaves
-- it here for the next run. There is no status column because there is no
-- third state worth keeping: a sent mail is in the ledger, and a dropped one
-- is nothing at all.
--
-- NO ADDRESS IN THIS TABLE, on migration 082's rule. The address is on the
-- Ticket, where the Holder Address Purge (migration 081) already reaches it;
-- the sweep reads it from there at send time, which is also the only way it
-- can notice the Ticket has changed hands since.
--
-- NOT RATIONED HERE AND NOT COUNTED HERE. The per-buyer rolling window never
-- applies to an assignment made at checkout, since the money has already moved
-- (ADR 0076); the per-Ticket lifetime allowance is spent by the ledger row the
-- sweep writes when this mail is actually sent, and not by this row.
CREATE TABLE owed_assignment_mails (
    -- The Ticket the mail is about. ONE OWED MAIL PER TICKET: the commit
    -- assigns each Ticket once, and a Ticket can only be owed the mail for the
    -- assignment it carries now.
    --
    -- ON DELETE CASCADE follows `tickets`, which in practice deletes nothing,
    -- for migration 082's reason.
    ticket_id UUID PRIMARY KEY REFERENCES tickets (id) ON DELETE CASCADE,

    -- The `tickets.assigned_at` of the assignment this mail is owed for, copied
    -- from the Ticket in the statement that assigned it.
    --
    -- IT IS WHAT MAKES A STALE ROW RECOGNISABLE. The Assignment Link is signed
    -- over assigned_at, and every reassignment - by the buyer, to anybody,
    -- including back to themself - moves it. So a sweep that finds the Ticket's
    -- assigned_at no longer equal to this value is holding a mail for an
    -- assignment that no longer exists, whose link would open nowhere, and
    -- drops it unsent. The Sale reversed and the Event started are read off the
    -- Sale and the Event at send time, and need no column here.
    assigned_at TIMESTAMPTZ NOT NULL,

    -- How many times a sweep has claimed this row, incremented BY THE CLAIM and
    -- not by the failure that follows it, as `follow_digests.attempt_count` is
    -- (migration 055): a sweep that dies mid-send has still used an attempt.
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),

    -- The earliest instant a sweep may claim this row. Written as the commit's
    -- instant, so a freshly owed mail is due at once; moved forward by every
    -- claim to hide the row from other sweeps for the claim's lease, and by
    -- every failure to whatever backoff the sweep chooses. A claim whose sweep
    -- vanished simply comes due again.
    next_attempt_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The sweep's claim: the owed mails that are due, oldest first.
CREATE INDEX owed_assignment_mails_due_idx
    ON owed_assignment_mails (next_attempt_at);
