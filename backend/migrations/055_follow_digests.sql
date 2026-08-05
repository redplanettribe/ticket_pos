-- The pending Follow Digest: one Customer's Digest for one week, waiting to be
-- composed and sent (#220, parent #215, ADR 0030).
--
-- Shaped after `sale_reversals` (migration 039) and for the same reasons, which
-- is not imitation: this is the platform's second piece of scheduled work and
-- the first that fans out to many recipients, and ADR 0024 already paid for the
-- shape once. A status, an `attempt_count`, a `next_attempt_at`, and partial
-- indexes sized to the working set rather than to the history.
--
-- WHY A QUEUE AT ALL, rather than one job that reads the Follows and mails them.
-- The mail provider's rate limit is roughly two requests per second, which
-- ADR 0009 records as unsolved for bulk sends; a single request that mailed
-- every Customer would exceed the Cloud Run request timeout long before it
-- exceeded the provider's patience. Splitting the work into rows is what lets a
-- per-minute drain move through it in bounded batches, and it is also what makes
-- a failed send RETRYABLE — a row that is still `pending` is a Customer who has
-- not been written to yet, which is a fact no in-memory loop could survive a
-- deployment with.
--
-- A ROW IS NOT A COMPOSED DIGEST. Nothing here records which Events the Digest
-- will carry, and that absence is load-bearing: the Digest is composed at SEND
-- time, not at enqueue time (see digest/service.deliverDigest). A Digest
-- enqueued on Thursday morning and drained an hour later reflects the world as
-- it is when it is sent, not as it was when the week was declared — an Event
-- published in between is in it, and an Event unpublished in between is not. If
-- a future reader adds a payload column here, that property goes away silently.
CREATE TABLE follow_digests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Whose Digest. CASCADE for the reason every Customer-owned row cascades: a
    -- pending Digest for a deleted Customer is mail addressed to nobody, and it
    -- is an input to a send.
    customer_id UUID NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    -- WHICH WEEK, as the date the week begins on: Monday, in Ecuador's zone.
    -- See platform.DigestWeekStart, which is the one place that rule is stated.
    --
    -- A DATE rather than a timestamp, because a week is a calendar fact and not
    -- an instant. Two enqueue runs on different days of one week must agree on
    -- the same value or the uniqueness below buys nothing, and a timestamp is a
    -- value two runs can disagree about by microseconds.
    week_start DATE NOT NULL,
    -- Where this Digest has got to:
    --
    --   pending  enqueued and not yet delivered. The working set: this is the
    --            only status the drain claims, and the only one the partial
    --            index below carries.
    --   sent     the message was accepted by the provider AND the sent-ledger
    --            rows for everything it carried are written. Both, in that
    --            order, in one transaction — see deliverDigest.
    --   empty    composed, and there was nothing to say. The Customer's Follows
    --            matched no eligible Event they had not already been shown, so
    --            NO EMAIL WAS SENT and no ledger row was written. This is a
    --            deliberate terminal state rather than a silent `sent`: an empty
    --            Digest must never reach an inbox (ADR 0030 — the Digest is the
    --            whole payload of a Follow, and a payload of nothing is how a
    --            Customer learns to ignore it), and recording WHY nothing was
    --            sent is the difference between that rule working and the send
    --            having failed unnoticed.
    --   failed   the platform gave up. Delivery kept failing past the attempt
    --            bound (digest/service.digestMaxAttempts), so this Customer got
    --            no Digest this week and will not get one now — the week has
    --            moved on, and a Digest arriving days late is worse than one
    --            that never came.
    status TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'empty', 'failed')),
    -- How many times delivery has been ATTEMPTED for this Digest. Counts
    -- attempts, not Events and not messages: the row is mutated in place, and
    -- one Digest is always one message however many Events it carries.
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    -- The earliest instant a drain may claim this Digest. Written at enqueue (as
    -- "now", so the first drain after the enqueue picks it up), then rewritten
    -- by every claim to hide the row from other claimants for the claim lease,
    -- and by every failure to the backoff its attempt count earned.
    --
    -- Meaningless once the status is terminal, which is why the index below
    -- reads it only for `pending` rows.
    next_attempt_at TIMESTAMPTZ NOT NULL,
    -- What went wrong last time, for whoever is reading the queue during an
    -- incident. Bounded exactly as sale_reversals.last_error is: a delivery
    -- failure is a line, and an unbounded column is an invitation to store a
    -- stack trace.
    last_error TEXT CHECK (char_length(last_error) <= 500),
    -- When the message was accepted by the provider. NULL on every status but
    -- `sent`, including `empty` — nothing was sent there, and a timestamp
    -- claiming otherwise would be the one lie this table could tell about
    -- whether a person was written to.
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- ONE DIGEST PER CUSTOMER PER WEEK, enforced by the database rather than by
    -- a check the application could race past. This is the constraint that makes
    -- a double-send IMPOSSIBLE rather than unlikely, and it is the single most
    -- important line in the file.
    --
    -- It holds against the two failures that actually happen. A weekly enqueue
    -- that runs twice — a retried Cloud Scheduler attempt, an operator curling
    -- the endpoint after the cron already fired, two overlapping runs — inserts
    -- the second row into a unique violation, which the enqueue swallows as ON
    -- CONFLICT DO NOTHING; there is no second row, so there is nothing for a
    -- drain to claim, so there is no second email. And two overlapping DRAIN
    -- ticks cannot both work one Customer's week, because there is only ever one
    -- row to claim and the claim takes it out of the queue.
    --
    -- NOT partial, unlike sale_reversals' live-request index. Every status
    -- counts here, terminal ones most of all: the whole point is that a `sent`
    -- row blocks a second Digest for that week, and a `failed` or `empty` one
    -- says the week was answered and must not be silently re-run into an inbox.
    UNIQUE (customer_id, week_start)
);

-- The drain's claim query: the pending Digests that are due, oldest first.
-- Partial so it stays the size of the backlog rather than the size of the
-- history — this table gains a row per following Customer per week forever, and
-- all but one week of it is terminal at any moment.
CREATE INDEX follow_digests_due_idx
    ON follow_digests (next_attempt_at) WHERE status = 'pending';
