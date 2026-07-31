-- A Payout Request may FAIL: the bank sent the transfer back (#185, #181,
-- ADR 0026 amendment).
--
-- Migration 045 put `failed` in the status CHECK and deliberately built no way
-- to reach it — the vocabulary was written once so a CHECK rewritten twice in
-- two weeks could not get the list wrong twice. This migration is the other
-- half: the rule that makes the state HABITABLE, which is that a failure says
-- why.
--
-- The state machine, unchanged in shape since 045 and now completely reachable:
--
--     pending ──→ processing ──→ paid
--        │            └────────→ failed
--        ├──→ paid          (unchanged — an instant transfer skips processing)
--        ├──→ declined
--        └──→ cancelled
--
-- `failed` IS ONLY REACHABLE FROM `processing`, and that guard lives in Go, in
-- the compare-and-swap's WHERE, rather than here. A CHECK constraint sees one
-- row at one instant and cannot see where it came from, so it can say what a
-- `failed` row must LOOK like and nothing about how it got that way. What it can
-- say — and does, below — is that a failure carries a reason and a resolution
-- stamp, which is the part a hand-written UPDATE could otherwise get wrong.
--
-- NO `payouts` ROW EXISTS FOR A FAILED REQUEST, and nothing in this migration
-- creates one or deletes one. There is nothing to unwind precisely because 045
-- refused to write the ledger row on submission: the money never moved, so the
-- books never claimed it did. That absence is asserted by COUNTING `payouts`
-- rows in the integration tests, never inferred from a status.
--
-- `failed` IS TERMINAL AND IS NOT RETRIED. The bank details on a request are a
-- frozen snapshot (migration 043) and a request cannot be edited, so the most
-- common failure — a wrong account number — is unfixable inside the request it
-- happened to, and reopening it to `pending` would have an operator retrying
-- against the same bad account forever. The Organization corrects its Payout
-- Profile and asks again, which the partial unique index already permits: it
-- counts `('pending', 'processing')` (migration 045), and a `failed` request is
-- neither, so the slot is free the instant the failure is recorded.

-- A resolution says why, whenever the answer was "not paid".
--
-- Migration 044 renamed decline_reason to resolution_reason and left THIS
-- constraint's name alone, saying in as many words that the ticket widening the
-- rule is where the name stops being true and should move. This is that ticket,
-- so the name moves with the rule, in the same statement that changes it.
--
-- The rule generalises to `status IN ('declined', 'failed')` and to nothing else.
-- The argument for a decline carrying one is unchanged — a queue that swallows
-- requests silently generates the support thread it was built to prevent — and
-- it holds identically for a failure: an organizer whose transfer bounced learns
-- nothing from the word "failed", and the reason is the entire content of the
-- news. Usually it is "the account number was rejected", which is also the
-- instruction: go and fix your Payout Profile.
--
-- IT IS STRUCTURAL AND NOT MERELY A SERVICE CHECK, for the same reason the
-- decline's was: the service is one writer among several — a migration, a
-- console session, a future job — and this rule is the difference between an
-- organizer reading a sentence and an organizer reading a blank.
--
-- `failed` AND `declined` STAY DISTINCT STATES SHARING ONE COLUMN, which is the
-- only place they are allowed to touch. A decline is a judgement a person made;
-- a failure is a bank sending money back and no judgement at all. They read
-- differently to the organizer, and any count or filter that collapses them
-- makes every decline-rate figure count bank errors as refusals (ADR 0026
-- amendment).
--
-- Nothing else is forbidden a reason that was not already forbidden one: a
-- `cancelled` or `paid` request may still carry a null one, and 043's 500-
-- character bound (now payout_requests_resolution_reason_check, migration 044)
-- is untouched.
--
-- DROP and re-ADD under the new name, rather than ALTER ... RENAME followed by a
-- change: renaming and rewriting are one behavioural change, and splitting them
-- across two statements would leave a moment where a constraint named for the
-- widened rule enforces the narrow one. The ADD re-validates every existing row
-- against the wider rule — no row can be `failed` today, since nothing could
-- reach the state, so the only rows it examines are the `declined` ones the old
-- constraint already validated, and it cannot fail.
ALTER TABLE payout_requests DROP CONSTRAINT payout_requests_decline_has_reason;
ALTER TABLE payout_requests ADD CONSTRAINT payout_requests_resolution_has_reason CHECK (
    status NOT IN ('declined', 'failed')
    OR (resolution_reason IS NOT NULL AND resolution_reason <> '')
);

COMMENT ON COLUMN payout_requests.resolution_reason IS
    'Why the request ended the way it did, shown to the asker. Required on ''declined'' and on ''failed'' by payout_requests_resolution_has_reason, and null everywhere else. The two are NOT the same fact wearing two labels: a decline is a judgement a person made, a failure is a bank returning the money (#185).';
