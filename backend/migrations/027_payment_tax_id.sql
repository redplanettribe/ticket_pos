-- The Tax ID on the Payment snapshot (#98, ADR 0016).
--
-- Migration 026 gave the Customer its current Tax ID and the Ticket Sale its
-- immutable snapshot. This one fills the gap between them on the online
-- channel: the Ticket Sale is recorded at *confirm* time, from the Payment, in
-- a request that carries nothing of the checkout form — the Payment Provider's
-- return redirect brings back a transaction id and nothing else. So every buyer
-- fact the sale records has to be snapshotted here at begin-checkout and read
-- back at confirm, exactly as customer_email and customer_first/last_name
-- already are (migration 019). The Tax ID travels the same road.
--
-- Nullable for the same reason every Tax ID column is (ADR 0016): Payments
-- recorded before this migration have none, and history is never backfilled.
-- The requirement that an Online Sale carry a valid Tax ID lives in the
-- checkout service path, not in this schema.
ALTER TABLE payments ADD COLUMN customer_tax_id_type TEXT
    CHECK (customer_tax_id_type IN ('cedula', 'ruc', 'passport'));
ALTER TABLE payments ADD COLUMN customer_tax_id_number TEXT;

-- Half a Tax ID is meaningless — the same pair rule migration 026 states on
-- customers and ticket_sales.
ALTER TABLE payments ADD CONSTRAINT payments_customer_tax_id_pair_ck CHECK (
    (customer_tax_id_type IS NULL) = (customer_tax_id_number IS NULL)
);

-- Whether the checkout that created this Payment ran under the buyer's own
-- Customer Session — that is, the begin request presented a full Customer
-- Session belonging to the very email being bought under.
--
-- This is the one fact that decides whether the sale's Tax ID may overwrite a
-- *verified* Customer's stored one (ADR 0016): a person editing their own
-- prefilled value is correcting themselves, while an anonymous visitor typing
-- a known email is not. It is recorded here rather than derived at confirm
-- because confirm has no session: it runs on the provider's redirect back,
-- which authenticates nobody.
--
-- NOT NULL with a false default: every historical Payment was anonymous by
-- construction (nothing sent a session with begin-checkout before this
-- migration), so "unknown" and "not session-authorized" are the same answer,
-- and the write-back rule reads a boolean rather than a tri-state.
ALTER TABLE payments ADD COLUMN customer_session_authorized BOOLEAN NOT NULL DEFAULT FALSE;
