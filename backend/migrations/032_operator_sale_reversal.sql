-- The Operator Reversal: a Platform Operator records an out-of-band refund
-- (#125, ADR 0018's sibling decision in #123).
--
-- The refund itself happens off-platform — by hand in the PayPhone dashboard,
-- or by bank transfer — and the platform never sees it. What lands here is the
-- operator's assertion that it happened: the same trust shape as a recorded
-- Payout, "a record of money that already moved". So the Ticket Sale gains a
-- third reversal actor and, beside it, the memo of what the operator asserted.

-- The third actor. Migration 031 called this out as the change it was leaving
-- room for — "a value added later, say a Platform Operator acting on an
-- incident, is a one-line constraint change, not a redesign" — and this is that
-- line. `operator` joins `customer` (the buyer's own undo inside the Reversal
-- Window) and `staff` (a Sale Import undo).
--
-- The constraint is dropped and re-added under its own name because 031 wrote
-- it inline, which left Postgres to name it.
ALTER TABLE ticket_sales DROP CONSTRAINT ticket_sales_reversed_by_check;
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_reversed_by_check
    CHECK (reversed_by IN ('customer', 'staff', 'operator'));

-- WHO, not just which side. `reversed_by` records the kind of actor, and for
-- the other two that is the whole answer available: a reversing Customer is the
-- sale's own buyer, already snapshotted on this row, and a Sale Import undo's
-- Member is on the batch. An Operator Reversal has neither — the acting person
-- is a Platform Operator who is a Member of nothing — and it is the one route
-- that asserts a money fact on human say-so. So it is never anonymous.
--
-- The operator's email, exactly as payouts.recorded_by records it, and for the
-- same reason: the allowlist is keyed by email (ADR 0015) and the session is
-- where the value comes from. Not a foreign key to platform_operators, because
-- the record must survive an operator being taken off the allowlist.
ALTER TABLE ticket_sales ADD COLUMN reversed_by_operator TEXT;

-- The free-text note: "refunded via PayPhone dashboard", "bank transfer, waived
-- fee as goodwill" — the parts no schema captures, kept for whoever reconciles
-- later. Optional and bounded: a note is a sentence, and an unbounded text
-- column on a row this hot is an invitation to paste a support thread into it.
ALTER TABLE ticket_sales ADD COLUMN reversal_note TEXT
    CHECK (char_length(reversal_note) <= 500);

-- What the buyer actually got back, in the sale's own currency's minor units.
-- Strictly positive: a reversal that returned nothing is not a refund, and
-- "zero refunded" must stay distinguishable from "nothing to refund" — which is
-- NULL, the free Online Sale's answer (#126).
--
-- It feeds no aggregate. There is no cash view for it to correct — the bank
-- balance has never been a system figure — and it is recorded so that a future
-- reconciliation can answer what actually left our account.
ALTER TABLE ticket_sales ADD COLUMN refunded_amount_cents INTEGER
    CHECK (refunded_amount_cents > 0);

-- Whether the platform kept its commission on the refunded sale. Deliberately
-- nullable with no default: the operator states it on every paid Operator
-- Reversal, and a default would put a decision about the platform's revenue in
-- the schema's mouth rather than a person's. The Platform Fee and its Fee IVA
-- always travel together, so this one flag decides both (the Fee Handling rule).
ALTER TABLE ticket_sales ADD COLUMN platform_fee_kept BOOLEAN;

-- All four are the Operator Reversal's own record, and belong to no other
-- route: a sale the buyer undid, or a Sale Import undo voided, carries none of
-- them. Stated as one constraint because they stand or fall together.
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_operator_reversal_ck CHECK (
    reversed_by = 'operator' OR (
        reversed_by_operator IS NULL
        AND reversal_note IS NULL
        AND refunded_amount_cents IS NULL
        AND platform_fee_kept IS NULL
    )
);

-- An Operator Reversal always names its operator. The note and the money memo
-- may each be absent — a note is optional, and the money fields are absent
-- rather than zero on a free Online Sale — but the person never is.
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_operator_identity_ck CHECK (
    reversed_by <> 'operator' OR reversed_by_operator IS NOT NULL
);

-- The two money facts travel together: an amount with no fee decision cannot
-- say what the platform kept, and a fee decision with no amount cannot say what
-- the buyer got. Both present on a paid sale, both absent on a free one.
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_operator_money_pair_ck CHECK (
    (refunded_amount_cents IS NULL) = (platform_fee_kept IS NULL)
);
