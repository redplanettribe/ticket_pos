-- Platform Operators: the allowlist that grants platform-wide authority (ADR
-- 0015). Authority is an email on this list, exercised through an ordinary Staff
-- Session — orthogonal to Membership, so an operator needs no Organization and
-- being an Org Admin grants nothing here.
--
-- Rows are inserted by direct database write in production (plus the dev seed in
-- 025). There is deliberately no UI and no API for managing operators: the first
-- row could never be added through one, and the action is rare enough that none
-- is warranted. Revoking is a DELETE, and it takes effect on the next request
-- without a deploy.

CREATE TABLE platform_operators (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The email is the identity: it is matched against the Staff Session's
    -- email, which is already normalised before a session is minted.
    email TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Who recorded a Payout, kept as plain text rather than a reference to the row
-- above: the audit trail must survive an operator's removal from the allowlist,
-- and a foreign key would either block the delete or erase the history. Rows
-- recorded before the Operator Dashboard existed (by hand, in psql) keep a NULL
-- recorder, which is the honest answer for them.
ALTER TABLE payouts
    ADD COLUMN recorded_by TEXT;
