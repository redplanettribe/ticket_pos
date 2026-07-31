-- `decline_reason` is about to stop being only about declines (#182, #181).
--
-- Migration 043 named this column after the single thing that could fill it: an
-- operator considering an ask and refusing it, with a sentence the asker reads.
-- A Payout Request is gaining a `failed` state — a transfer an operator did
-- submit, that the bank sent back, most often because the account number was
-- wrong — and the reason a failure carries goes in the same column. It has to:
-- it is the same fact from the reader's side, "here is why you were not paid",
-- and a second nullable free-text column holding the other half of one sentence
-- would leave every renderer coalescing between them.
--
-- So the column is renamed BEFORE either meaning arrives. A decline is a
-- judgement a person made; a failure is a bank returning money and no judgement
-- at all. Code written against `decline_reason` while it holds both would read
-- as a claim the platform refused someone over a typo — and a name that has
-- already fanned out across a row struct, two service types, a JSON field, a
-- generated client and a UI only gets more expensive to correct. This migration
-- is deliberately alone: nothing else changes, so the diff above it is
-- reviewable as exactly the mechanical rename it is.
--
-- This is a breaking change to a response body, taken knowingly. The only
-- consumer is the staff app, whose typed client is generated from this server
-- and ships with it.
--
-- RENAME COLUMN rather than add-copy-drop. Postgres rewrites the column's
-- references inside every constraint and index that names it, in the same
-- transaction, so the 500-character bound and the reason-required-on-a-decline
-- rule survive untouched and there is no window in which a row could be written
-- past them. No data moves and no row is rewritten.
ALTER TABLE payout_requests RENAME COLUMN decline_reason TO resolution_reason;

-- The 500-character bound was written inline on 043's column definition, so
-- Postgres named it after the column. A rename carries the EXPRESSION across but
-- leaves the identifier as it was, which would leave the schema carrying a
-- constraint named after a column that no longer exists — the kind of small lie
-- that makes the next reader of `\d payout_requests` doubt everything else on
-- the page. The new name is the one Postgres would have chosen had the column
-- always been called this.
ALTER TABLE payout_requests
    RENAME CONSTRAINT payout_requests_decline_reason_check TO payout_requests_resolution_reason_check;

-- payout_requests_decline_has_reason keeps its name, deliberately. It is still a
-- rule ABOUT DECLINES — a declined request is the only one that must carry a
-- reason today, and this migration changes no behaviour — so the name is still
-- true. The ticket that adds `failed` widens the rule to cover it, and that is
-- when the name stops being true and should move. Renaming it here would put a
-- behavioural change's paperwork in a migration that has no behavioural change.

COMMENT ON COLUMN payout_requests.resolution_reason IS
    'Why the request ended the way it did, shown to the asker. Today only a decline fills it, and the payout_requests_decline_has_reason CHECK requires one there; a bank rejection will fill it too (#181).';
