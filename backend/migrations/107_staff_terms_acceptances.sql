-- Staff Terms Acceptances: the Staff platform's first and only consent gate
-- (#538, parent #533, ADR 0066).
--
-- One row per person per Terms edition, in a CAPACITY: everyone signing into
-- the Staff platform — org_admin, event_owner, event_staff, and Platform
-- Operators alike — accepts the Términos y Condiciones "en calidad de
-- organizador" (§3) before a Staff Session is minted. The table is APPEND-ONLY
-- for the reason consent_records is: it is evidence of a contractual act, and
-- no update or delete may rewrite what was accepted. Nothing in the codebase
-- issues an UPDATE or DELETE against it, and nothing may.
--
-- KEYED ON EMAIL, not on members(id), and the departure is migration 067's,
-- argued there for `recorded_by`: the person key of the Staff platform is the
-- email a session names. A Platform Operator may hold no members row at all,
-- a person with three Organizations holds three, and the acceptance belongs to
-- the PERSON — one acceptance per email per edition, however many memberships
-- hang off it. staff_locales made the same choice for the same reason.
--
-- NO CURRENT-STATE COLUMN ANYWHERE — no accepted_at on a person row, no
-- is-current flag. Outstanding is computed as "no row for this email matching
-- the current edition": one indexed lookup at sign-in, and inserting a later
-- terms_versions row re-gates everybody with no data migration, exactly as it
-- does the Customer base (#536).
CREATE TABLE staff_terms_acceptances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The person, as the Staff Session names them: normalised email, never
    -- blank. No foreign key, for migration 067's reasons — there is nothing on
    -- the Staff platform that IS the person to reference.
    email TEXT NOT NULL CHECK (email <> ''),
    -- Which edition was accepted. ON DELETE RESTRICT: deleting a Terms Version
    -- that anybody accepted would orphan evidence, so it is refused — the same
    -- posture consent_records takes toward policy_versions.
    terms_version_id UUID NOT NULL REFERENCES terms_versions (id) ON DELETE RESTRICT,
    -- The capacity the acceptance was made in (§3, ADR 0066). 'organizer' is
    -- the whole vocabulary today — every human on the Staff platform accepts in
    -- it — and the CHECK is how the vocabulary stays OPEN: a later 'staff'
    -- capacity is a constraint swap (the channel pattern, migration 067), not a
    -- schema redesign, and rows for two capacities can coexist under the UNIQUE
    -- below without churn.
    capacity TEXT NOT NULL CHECK (capacity IN ('organizer')),
    -- When the box was ticked, by the server clock that gated the sign-in.
    accepted_at TIMESTAMPTZ NOT NULL,
    -- The technical proof, mirroring consent_records column for column: all
    -- four nullable, corroboration rather than proof, TEXT and not INET for
    -- migration 061's reasons. `session_id` names the Staff Session this
    -- acceptance MINTS — the only identifier that ties the row to what the
    -- person did next — and carries no foreign key because sessions are
    -- deleted at sign-out and evidence must not go with them.
    ip TEXT,
    user_agent TEXT,
    session_id TEXT,
    origin_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One acceptance per email per edition per capacity — the rule (#538) made a
-- constraint, and the one indexed lookup the sign-in gate performs. UNIQUE
-- rather than a plain index so a double-submit race appends one row, not two:
-- the second insert is a no-op, never a second piece of evidence.
CREATE UNIQUE INDEX staff_terms_acceptances_email_version_capacity_idx
    ON staff_terms_acceptances (email, terms_version_id, capacity);

-- Proof of Email Ownership in suspension, for the terms step: the staff
-- counterpart of pending_consents (migration 063), and everything argued there
-- holds — the token is the primary key so presenting it IS the lookup, spending
-- it is a DELETE so there is nothing to replay, and the window is minutes
-- because it only has to outlast reading a checkbox label and following one
-- link. It exists because the passcode is spent at verification: a sign-in
-- gated on the Terms has proven its address and must be able to finish without
-- proving it again.
--
-- No customer_id counterpart to reference: the email IS the person here, copied
-- in as the address that was proven, which is what the acceptance row this
-- token leads to must assert.
CREATE TABLE pending_staff_terms (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL CHECK (email <> ''),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Supports sweeping abandoned rows, the posture pending_consents ships with:
-- nothing sweeps today, rows are deleted on redemption, and an abandoned one is
-- unusable the moment it expires.
CREATE INDEX pending_staff_terms_expires_at_idx ON pending_staff_terms (expires_at);
