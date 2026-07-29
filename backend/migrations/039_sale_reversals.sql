-- The Reversal Request: a Customer's ask to undo their own paid Online Sale,
-- recorded before the Payment Provider is called and pursued until the provider
-- gives a definite answer (#157, ADR 0024). This table is the thing to come back
-- to when the provider says nothing; the order that makes it work is enacted and
-- explained in sales/service.ReverseOwnSale.
--
-- Migration 031 deferred exactly this table — "not a `sale_reversals` audit
-- table: ADR 0018 weighed the table and deferred it as disproportionate until
-- the feature has volume" — and this is not that table arriving late. The
-- provenance of a COMPLETED reversal still lives on ticket_sales, where 031 and
-- 032 put it. What lives here is an ask whose answer is not yet known: a record
-- with a lifecycle, a retry schedule and an error, which is what those two
-- columns could never have been.
--
-- Deliberately NOT a third ticket_sales.status value. Seven aggregates filter
-- `ts.status = 'active'` — affiliate commission, the Operator Dashboard, the
-- Withdrawable Balance, event sales, capacity, import undo and ReverseSales
-- itself — and while a Reversal Request is in flight the sale must keep counting
-- in every one of them, because no money is known to have moved (ADR 0024).
CREATE TABLE sale_reversals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The Ticket Sale being undone. CASCADE for the same reason every other
    -- child of a sale cascades: a Reversal Request for a sale that no longer
    -- exists is an ask about nothing.
    ticket_sale_id UUID NOT NULL REFERENCES ticket_sales (id) ON DELETE CASCADE,
    -- The Payment's id under OUR namespace, which is what PaymentProvider.Reverse
    -- is keyed on. Snapshotted here rather than re-derived from `payments` at
    -- every probe: this row exists to be re-asked about days later, and the
    -- transaction it names is somebody's money — the platform must ask about the
    -- same one every time, not about whichever Payment a later query returns.
    --
    -- NOT NULL and never empty. A Reversal Request is only ever made for a paid
    -- Online Sale; a free one has no provider to wait on and creates no row here
    -- at all (ADR 0024).
    client_transaction_id TEXT NOT NULL CHECK (client_transaction_id <> ''),
    -- When the Customer pressed Undo — the instant that had to sit inside the
    -- Reversal Window, and the record of the authorisation itself.
    --
    -- It is NOT ticket_sales.reversed_at, and the two must never be conflated.
    -- This is when the buyer asked; that stays what it has always been, the
    -- moment the platform learned the reversal succeeded. Keeping both is what
    -- lets a retry at 20:05 continue a request made at 19:58 without ever
    -- re-checking a Window it would now fail (ADR 0024).
    requested_at TIMESTAMPTZ NOT NULL,
    -- Where the ask has got to:
    --
    --   in_flight       the provider has not given a definite answer yet. The
    --                   Ticket Sale stays active and its capacity stays held.
    --   succeeded       the provider answered `true`, or errorCode 24 — a
    --                   receipt that the money already left. The Ticket Sale
    --                   flips to reversed through the shared ReverseSales
    --                   primitive.
    --   refused         the provider considered it and said no. Nothing
    --                   happened; the Ticket Sale is untouched.
    --   needs_attention an Unresolved Reversal: the platform gave up asking with
    --                   the answer still unknown, and only the provider's own
    --                   dashboard can say. Written today when the Ticket Sale is
    --                   reversed by somebody else mid-flight, since asking again
    --                   could refund the buyer twice; the Reversal Reconciler
    --                   that also gives up after a day of silence is #158.
    status TEXT NOT NULL CHECK (status IN ('in_flight', 'succeeded', 'refused', 'needs_attention')),
    -- How many times the provider has been asked about THIS request. Counts
    -- probes, not presses: the row is mutated in place, one row per reversal
    -- rather than one per attempt, because the question being asked is always
    -- the same question.
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    -- The earliest instant the platform may ask about this request again.
    -- Written on every unresolved attempt and READ by every drain, which is what
    -- keeps a Customer refreshing their Area during a provider outage from
    -- re-posting Reverse on every render. Meaningless once the status is
    -- definite, which is why the partial index below reads it only for in_flight
    -- rows.
    --
    -- What ships now is a single flat delay after an unknown answer. The full
    -- backoff schedule — 10s, 30s, 2m, 5m, 15m, then every 30m with jitter —
    -- belongs to the Reversal Reconciler (#158) and changes only what is written
    -- here, never how it is read.
    next_attempt_at TIMESTAMPTZ NOT NULL,
    -- What the provider last said, for the operator reading the queue during an
    -- incident. Bounded like reversal_note (migration 032): a provider error is
    -- a line, and an unbounded column is an invitation to store a stack trace.
    last_error TEXT CHECK (char_length(last_error) <= 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- At most one LIVE Reversal Request per Ticket Sale, enforced by the database
-- rather than by a check the application could race past. A second press while
-- one is in flight has nowhere to write, so it can only ever read the existing
-- row's pending state — which is exactly what the endpoint does.
--
-- `status <> 'refused'` is THE definition of a live request, and every reader
-- that asks "does this sale already have an ask?" quotes it rather than
-- narrowing it. A reader using a narrower predicate concludes there is no
-- request where this index will refuse to let it write one, and the buyer gets a
-- unique violation instead of an answer.
--
-- `refused` is excluded, and that is the whole reason this is partial rather
-- than a plain unique index. A refusal means nothing happened, so a buyer still
-- inside their Reversal Window is entitled to a genuinely new ask, and a row
-- recording that the platform once said no must not stand in its way.
--
-- `succeeded` and `needs_attention` are NOT excluded. A succeeded request is the
-- reversal that happened, and there is nothing left to ask about; an Unresolved
-- Reversal is a question a Platform Operator has to settle, and a second ask
-- while the first is unanswered would be the double refund the whole design
-- exists to avoid.
CREATE UNIQUE INDEX sale_reversals_live_per_sale_key
    ON sale_reversals (ticket_sale_id) WHERE status <> 'refused';

-- The Reversal Reconciler's claim query (#158) and the opportunistic drain that
-- ships now: the in-flight requests due to be asked about, oldest first. Partial
-- so it stays the size of the backlog rather than the size of the history.
CREATE INDEX sale_reversals_due_idx
    ON sale_reversals (next_attempt_at) WHERE status = 'in_flight';
