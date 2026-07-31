-- The Payout Request: an Organization asking to be paid, and the Platform
-- Operator answering by transferring the money and recording a Payout (#175,
-- ADR 0026). A Payout has always been a record rather than an instruction
-- (ADR 0014) — the platform settles off-platform and an operator writes down
-- that it happened — and what was never modelled is the half that comes first.
-- Until now an Organization wanting its money asked in a WhatsApp thread, where
-- a request can be missed, answered twice, or answered by two operators who did
-- not know about each other. This table is that ask, stored.
--
-- A REQUEST MOVES NOTHING AND COUNTS FOR NOTHING. No aggregate, no balance and
-- no revenue figure knows this table exists: the Withdrawable Balance stays what
-- it has always been — Net Proceeds minus recorded Payouts — and requests are a
-- queue OVER that ledger, never a second one. A system with two places money can
-- be said to have moved has no answer to which one is true. Every future reader
-- adding a sum here should stop: the ledger is `payouts`, and the way a request
-- becomes money is `payout_id` below pointing at a row in it.
--
-- Both the amount and the bank details are frozen the moment the ask is made.
-- The profile in organization_payout_profiles (migration 042) says where to pay
-- TODAY; the six snapshot columns here say where an operator was TOLD to pay,
-- and no later edit of the profile can rewrite them. This is the house rule
-- already applied to fee amounts on a sale line (ADR 0014) and to the buyer's
-- Tax ID (ADR 0016), and it matters more here than in either: this is a money
-- instruction, and an Organization that changes banks must not retroactively
-- change the account a completed transfer was aimed at.
--
-- It lives in the `sales` module beside `payouts` and the profile, for the same
-- reason they do: a Payout Request is meaningless except for payouts, and
-- `identity` — which owns organizations, members and sessions — must not learn
-- what a bank account or a factura is. `organizations` gains no columns here.
--
-- The whole column set is written now, including the four that only #177 can
-- reach. `pending` and `cancelled` are the only states this migration's feature
-- produces, but a schema that has to be migrated again to record an answer would
-- make the answer the risky part, and `payout_id` in particular is the join that
-- says a request became money.
CREATE TABLE payout_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Whose ask it is. CASCADE for the reason every child of an Organization
    -- cascades: an ask by an Organization that no longer exists is an ask about
    -- nothing. The Payout it may eventually point at cascades the same way, so
    -- the two never survive each other.
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- What was asked for, in the Organization's currency — the same cents the
    -- Payout that answers it will record. Strictly positive: an ask for nothing
    -- is not an ask, and a negative one is an assertion about a direction of
    -- travel this table does not model.
    --
    -- It is deliberately NOT bounded here by anything to do with a balance. The
    -- cap is the Payable Balance at the moment of asking, checked once in the
    -- service and never again (ADR 0026): the balance moves afterwards, and a
    -- constraint that re-decided it would refuse to keep a record that was
    -- perfectly valid when it was written.
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    -- Whatever the form does not capture — "before the festival, please" — bound
    -- like every other operator-read free-text field in this schema. Nullable
    -- because most asks say nothing, and an empty string pretending to be
    -- silence is a value somebody eventually has to test for.
    note TEXT CHECK (char_length(note) <= 500),
    -- Where the ask has got to:
    --
    --   pending    nobody has answered. The one state the partial unique index
    --              below treats as outstanding.
    --   paid       an operator transferred the money and recorded the Payout
    --              `payout_id` names (#177).
    --   declined   an operator considered it and said no, with a reason the
    --              asker is shown.
    --   cancelled  the Organization withdrew it, which is the only way a pending
    --              request changes: it cannot be edited, only cancelled and
    --              re-asked, and that is what keeps `pending` genuinely singular.
    --
    -- There is deliberately no `approved`. An operator transfers the money and
    -- then records it, exactly as they always have; an approved-but-unpaid
    -- request is a debt with a state name, needing its own chasing, its own
    -- notifications and its own aging report (ADR 0026).
    status TEXT NOT NULL CHECK (status IN ('pending', 'paid', 'declined', 'cancelled')),
    -- Who asked, as an EMAIL and not a member id — exactly as payouts.recorded_by
    -- records an operator (migration 024). The record outlives the Membership:
    -- the person who asked for the money is a fact about the request forever,
    -- and a Member row that is later removed must not take it with them.
    requested_by TEXT NOT NULL CHECK (requested_by <> ''),
    -- The Payable Balance as it stood at the instant of asking, which is what
    -- later lets an operator tell "asked for all of it" from "asked for four
    -- times what they have". Signed and unclamped, exactly like the live figure:
    -- an Organization settled against money that had not cleared has a negative
    -- Payable Balance, and rounding that to zero here would make the snapshot
    -- disagree with the surface it was copied from (ADR 0026).
    payable_balance_cents INTEGER NOT NULL,
    -- The six-column snapshot of the Payout Profile. Every one is NOT NULL and
    -- carries the same CHECK its column in organization_payout_profiles does
    -- (migration 042), because a request exists only where a COMPLETE profile
    -- did: an incomplete one is a request an operator cannot action, which is
    -- the support thread this feature was built to remove.
    --
    -- The constraints are restated rather than shared. There is no mechanism in
    -- Postgres to inherit them, and stating them is what makes this table
    -- readable as what it is — a frozen copy — rather than as a set of columns
    -- that happen to look similar.
    bank_name TEXT NOT NULL CHECK (bank_name <> '' AND char_length(bank_name) <= 120),
    account_type TEXT NOT NULL CHECK (account_type IN ('ahorros', 'corriente')),
    -- Digits only and TEXT, so the leading zeros that reach the right account
    -- survive the round trip.
    account_number TEXT NOT NULL CHECK (account_number ~ '^[0-9]{1,34}$'),
    account_holder_name TEXT NOT NULL CHECK (account_holder_name <> '' AND char_length(account_holder_name) <= 120),
    -- `cedula` or `ruc` and never `passport`: this asks who is being invoiced
    -- and wired to, and a passport holder has no Ecuadorian bank account to
    -- receive it (ADR 0026).
    tax_id_type TEXT NOT NULL CHECK (tax_id_type IN ('cedula', 'ruc')),
    tax_id_number TEXT NOT NULL CHECK (tax_id_number <> '' AND char_length(tax_id_number) <= 20),
    -- The Payout that answered it, which is the ONLY link between this queue and
    -- the ledger. Nullable because it is null for every state but `paid`, and
    -- SET NULL rather than CASCADE on delete: a Payout deleted by hand during an
    -- incident must not silently take the record of the ask with it.
    payout_id UUID REFERENCES payouts (id) ON DELETE SET NULL,
    -- Why an operator said no, shown to the asker. A decline that swallows the
    -- request silently generates the support thread the queue was built to
    -- prevent, so the CHECK below makes the reason structurally inseparable from
    -- the refusal.
    decline_reason TEXT CHECK (char_length(decline_reason) <= 500),
    -- Who ended it and when: an operator's email for `paid` and `declined`, the
    -- cancelling Org Admin's for `cancelled`. An email again, for the same
    -- reason requested_by is one.
    resolved_by TEXT CHECK (resolved_by <> ''),
    resolved_at TIMESTAMPTZ,
    -- When the ask was made. This is `requested_at` by another name and the
    -- column the operator queue orders on, oldest first (#177).
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A decline says why, always. Enforced here rather than in the service
    -- because an operator's refusal reaching the asker as a blank is the exact
    -- failure the state was added to prevent.
    CONSTRAINT payout_requests_decline_has_reason CHECK (
        status <> 'declined' OR (decline_reason IS NOT NULL AND decline_reason <> '')
    ),
    -- Pending means unanswered, and answered means resolved. Keeping the two in
    -- step structurally is what lets every reader trust `status` alone rather
    -- than checking a timestamp beside it.
    CONSTRAINT payout_requests_resolution_matches_status CHECK (
        (status = 'pending') = (resolved_at IS NULL)
        AND (status = 'pending') = (resolved_by IS NULL)
    ),
    -- A Payout is named only by the state that means one was recorded. A
    -- `declined` request pointing at a settlement would say the platform both
    -- refused and paid.
    CONSTRAINT payout_requests_payout_only_when_paid CHECK (
        status = 'paid' OR payout_id IS NULL
    )
);

-- ONE OUTSTANDING REQUEST PER ORGANIZATION, enforced by the database rather than
-- by a check the application could race past — in the shape ADR 0024 already
-- uses for the Reversal Request (migration 039).
--
-- Without it the cap means nothing: three individually valid $1,000 requests
-- against $1,000 payable would put the arithmetic the system refused to do back
-- on the operator. A second submission has nowhere to write, so it can only read
-- the existing row and hand it back — which is exactly what the endpoint does,
-- the same courtesy the Reversal Request extends to a buyer pressing Undo twice.
--
-- It is PARTIAL, and only `pending` is outstanding. The three end states are all
-- final and none of them stands in the way of a new ask: an Organization whose
-- request was declined, cancelled or paid is entitled to ask again, and a row
-- recording what happened last time must not be what stops them.
CREATE UNIQUE INDEX payout_requests_one_pending_per_organization_key
    ON payout_requests (organization_id) WHERE status = 'pending';

-- The Organization's own request history, newest first, beside its payout
-- history on the Settings page.
CREATE INDEX payout_requests_by_organization_idx
    ON payout_requests (organization_id, created_at DESC);

-- The operator's cross-Organization queue, oldest first (#177). Partial so it
-- stays the size of the backlog rather than the size of the history, exactly as
-- the Reversal Reconciler's due index is. Oldest-first breaks the newest-first
-- house convention deliberately: this is a work queue, not a history, and the
-- oldest unanswered request is the one about to become a complaint (ADR 0026).
CREATE INDEX payout_requests_pending_queue_idx
    ON payout_requests (created_at) WHERE status = 'pending';
