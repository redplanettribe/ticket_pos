-- A Payout Request may be PROCESSING: the transfer was submitted and the bank
-- has not confirmed it (#184, #181, ADR 0026 amendment).
--
-- Migration 043 assumed a bank transfer either happens or does not, fast enough
-- that an operator transfers and records in one sitting. PayPhone does not work
-- that way: a transfer can take up to 48 hours to reach the Organization's
-- account, and it can come back rejected — most often because the account number
-- is wrong. That left an operator with no honest state to be in. They could
-- record a Payout for money that had not arrived, corrupting a ledger whose whole
-- meaning is MONEY THAT MOVED (ADR 0014), or leave the request `pending` and lose
-- the fact that a transfer was already out there — which is how one request gets
-- transferred twice.
--
-- The state machine after this migration:
--
--     pending ──→ processing ──→ paid
--        │            └────────→ failed
--        ├──→ paid          (unchanged — an instant transfer skips processing)
--        ├──→ declined
--        └──→ cancelled
--
-- NO `payouts` ROW EXISTS WHILE A REQUEST IS `processing`. Nothing in this
-- migration writes to the ledger and nothing may be added that does: a Payout has
-- meant money that moved since ADR 0014, so writing one on submission would mean
-- deleting or negating ledger rows when the bank sends the money back, and
-- inventing a voided-Payout concept every balance in the system would then have
-- to understand. The transfer is recorded HERE, on the request, and becomes a
-- Payout only when it lands.
--
-- `failed` is in the status CHECK below although nothing can reach it yet. The
-- transition — a compare-and-swap guarded on `processing`, with the reason its
-- asker reads — is #185's, and the reason-required rule widens to cover it there
-- (see payout_requests_decline_has_reason, deliberately untouched here: it is
-- still true, because a decline is still the only state that must carry a
-- reason). The VOCABULARY is written once, now, because a CHECK constraint
-- rewritten twice in two weeks is two chances to get the list wrong.
--
-- `processing` is NOT the `approved` state ADR 0026 rejected. That rejection was
-- of a state recording an operator's INTENTION to transfer — a promise the
-- platform then has to keep, needing its own chasing and its own aging report.
-- This one records something that has already happened, in the world, outside
-- the platform's control: an `approved` request is resolved by the platform doing
-- what it said it would, while a `processing` request is resolved by the platform
-- FINDING OUT what the bank did. Nothing chases it because nothing can.

-- The transfer, as the operator who submitted it knows it.
--
-- transfer_submitted_by is an EMAIL, exactly as requested_by, resolved_by and
-- payouts.recorded_by are (migrations 024, 043): the record outlives the Member.
-- It is a separate actor from resolved_by ON PURPOSE — the operator who submits
-- the transfer and the operator who confirms it need not be the same person, and
-- resolved_by must keep meaning who ENDED the request. Whoever answers a
-- three-day-old `processing` request needs to know which colleague to ask about
-- it, and that is a different question from who eventually closed it.
--
-- transfer_reference is whatever the bank handed back. OPTIONAL, because it is
-- not always given synchronously, and a required field an operator cannot fill is
-- a field they will type `-` into — at which point the column holds noise that
-- looks like data. Bounded at 200 like every other operator-read free-text field
-- here, and it stays on the REQUEST rather than on `payouts`: the ledger is
-- shared with the direct-record path, which has no request and no reference, and
-- widening it is a larger change than this needs.
ALTER TABLE payout_requests
    ADD COLUMN transfer_submitted_by TEXT CHECK (transfer_submitted_by <> ''),
    ADD COLUMN transfer_submitted_at TIMESTAMPTZ,
    ADD COLUMN transfer_reference TEXT CHECK (char_length(transfer_reference) <= 200);

-- THE MOST LOAD-BEARING LINES IN THIS MIGRATION.
--
-- `outstanding` stops meaning `pending`. A request whose transfer is in flight
-- still occupies the Organization's single slot: without this widening an
-- Organization could immediately ask again for money already on its way to them,
-- because the first request would no longer be `pending` and NO BALANCE HAS MOVED
-- to stop them — a request moves nothing and counts for nothing (migration 043),
-- so the index is the only thing standing there.
--
-- THE ORDER OF THE STATEMENTS IN THIS FILE IS THE PROOF THAT THE RECREATE IS
-- SAFE, and it must not be rearranged. Both indexes are recreated HERE, WHILE THE
-- OLD STATUS CHECK IS STILL IN FORCE — that constraint permits only ('pending',
-- 'paid', 'declined', 'cancelled'), and Postgres has validated it against every
-- row in the table. So at this point in the transaction NO ROW CAN BE
-- `processing`, `status IN ('pending', 'processing')` provably selects exactly
-- the rows `status = 'pending'` selected, and the unique index cannot collide on
-- a duplicate. The database proves it rather than the author asserting it. Widen
-- the status CHECK first and that proof is gone: the recreate would then be a
-- claim about production data, checked only by running it.
--
-- The unique index is RENAMED as well as widened, because its old name states a
-- RULE that this migration makes false. "one pending per organization" was the
-- rule; "one outstanding per organization" is. A name that lies about the
-- invariant it enforces is exactly the kind of small untruth that makes the next
-- reader of `\d payout_requests` doubt everything else on the page — and this is
-- the index the next reader is most likely to "simplify" back.
--
-- Nothing references either index by name: `ON CONFLICT (organization_id) WHERE
-- <predicate>` infers a partial index from its COLUMNS AND ITS PREDICATE, never
-- from its name. The predicate is stated once in Go, as
-- repository.outstandingPayoutRequest, and the two must stay in step or the
-- INSERT stops finding an arbiter — loudly, at runtime, on the first ask.
DROP INDEX payout_requests_one_pending_per_organization_key;
CREATE UNIQUE INDEX payout_requests_one_outstanding_per_organization_key
    ON payout_requests (organization_id) WHERE status IN ('pending', 'processing');

-- The operator's queue index, widened to match the queue's new WHERE. A transfer
-- an operator submitted and nobody confirmed is precisely the work that must not
-- fall out of sight, so the queue lists it and this index has to cover it or the
-- queue quietly stops using an index at all.
--
-- The NAME stays. Unlike the unique index above it names no rule — it names the
-- query it was built for, which is still ListPendingPayoutRequests, still feeding
-- a badge whose public field is `pending_count`. That name becomes wrong when the
-- API-level rename happens, and it should move then, with its callers.
DROP INDEX payout_requests_pending_queue_idx;
CREATE INDEX payout_requests_pending_queue_idx
    ON payout_requests (created_at) WHERE status IN ('pending', 'processing');

-- Now, and only now, the vocabulary widens.
--
-- Dropping and re-adding rather than any in-place edit, because Postgres has no
-- in-place edit for a CHECK: the ADD re-validates every existing row against the
-- new list, which is a strictly wider one, so it cannot fail. The name is the one
-- Postgres gave the inline CHECK in migration 043 and is kept, because the
-- constraint is still exactly what it was — the list of states a request may be
-- in.
ALTER TABLE payout_requests DROP CONSTRAINT payout_requests_status_check;
ALTER TABLE payout_requests ADD CONSTRAINT payout_requests_status_check
    CHECK (status IN ('pending', 'processing', 'paid', 'declined', 'cancelled', 'failed'));

-- Unresolved now means `status IN ('pending', 'processing')`.
--
-- Migration 043 wrote this as "pending means unanswered, and answered means
-- resolved", keeping status and the resolution stamp in step structurally so
-- every reader can trust `status` alone rather than checking a timestamp beside
-- it. That is still the rule; there are simply two states in which a request is
-- unanswered now. A `processing` request has NO resolved_by and NO resolved_at —
-- it has not been resolved, it has been ACTED ON, and who acted is the separate
-- pair of columns added above. Collapsing the two would make resolved_by mean
-- "the last operator to touch this", which is not a fact anybody needs.
ALTER TABLE payout_requests DROP CONSTRAINT payout_requests_resolution_matches_status;
ALTER TABLE payout_requests ADD CONSTRAINT payout_requests_resolution_matches_status CHECK (
    (status IN ('pending', 'processing')) = (resolved_at IS NULL)
    AND (status IN ('pending', 'processing')) = (resolved_by IS NULL)
);

-- The transfer stamp is whole or absent, and it agrees with the status.
--
-- Two rules, one constraint, because they are one idea: a submitted transfer is a
-- fact with a who, a when and a state, and half of one is a row nobody can
-- explain.
--
--   - WHO AND WHEN TRAVEL TOGETHER. A transfer submitted by nobody, or at no
--     time, is not a record of anything, and every surface reading one would have
--     to coalesce around the hole.
--   - THE REFERENCE IMPLIES THE PAIR. It is the bank's name for a transfer, so a
--     reference on a request nobody submitted a transfer for is a string with no
--     referent.
--   - A `pending` REQUEST HAS NEITHER, and a `processing` one has BOTH. Those are
--     the two directions of the same fact: the stamp is what marking a request
--     processing writes, so a `pending` row carrying one would mean a transfer was
--     submitted and the state forgot, and a `processing` row without one would
--     mean a transfer is in flight and nobody knows whose.
--
-- A request that went `pending → paid` DIRECTLY has neither, which is legal and
-- stays legal: an instant transfer skips `processing`, and requiring the stamp on
-- `paid` would make a state describing uncertainty into a ritual operators click
-- through. `paid`, `failed`, `declined` and `cancelled` are all deliberately
-- unconstrained here — a `paid` request may carry the stamp (it came through
-- `processing`) or not (it did not), and both are true things.
ALTER TABLE payout_requests ADD CONSTRAINT payout_requests_transfer_submission_is_whole CHECK (
    (transfer_submitted_by IS NULL) = (transfer_submitted_at IS NULL)
    AND (transfer_reference IS NULL OR transfer_submitted_by IS NOT NULL)
    AND (status <> 'pending' OR transfer_submitted_by IS NULL)
    AND (status <> 'processing' OR transfer_submitted_by IS NOT NULL)
);

COMMENT ON COLUMN payout_requests.transfer_submitted_by IS
    'The operator who submitted the bank transfer, as an email. Not the same fact as resolved_by, which is whoever ENDED the request — the two may be different people days apart (#184).';
COMMENT ON COLUMN payout_requests.transfer_submitted_at IS
    'When the transfer was submitted. The clock is the service''s injected one, never NOW(), so tests and the 72-hour stale flag read the same instant the rest of the system does.';
COMMENT ON COLUMN payout_requests.transfer_reference IS
    'Whatever the bank handed back for the transfer, if anything. Optional by design: a required reference an operator cannot fill becomes a column full of dashes.';
