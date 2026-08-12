-- Pending Consents: the short-lived, single-use credential that stands between
-- Proof of Email Ownership and a Customer Session (#251, parent #249).
--
-- A Customer with no Policy Acceptance of the current Policy Version proves
-- they own their address exactly as before — a passcode, or a Google Sign-In —
-- and is minted NO SESSION. What comes back instead is a row from this table:
-- a token that says "this address was proven a moment ago, and the only thing
-- it can buy is a consent submission". The submission records the evidence and
-- then mints the session the sign-in would have minted. Abandoning the step
-- leaves the person exactly as signed out as they were.
--
-- A SERVER-SIDE ROW AND NOT A SIGNED BLOB, which is the house pattern for every
-- credential (`sessions`, `customer_sessions`, `otp_challenges`) and is what
-- buys the two properties this token has to have. SINGLE USE: redemption
-- deletes the row, so a replay of the same request finds nothing — a signed
-- token would need a spent-token table to say the same thing, which is this
-- table with extra steps. SHORT LIVED: an expiry that is checked against a row
-- can also be revoked, and a proof of ownership that has been sitting in a tab
-- for a day should not still be spendable. The signed-token pattern earns its
-- place where a credential must survive without a database round trip and
-- outlive any session — the Confirmation Link and the unsubscribe link — and
-- this is the opposite case on both counts.
--
-- WHY IT LIVES IN `customers` AND NOT IN THE CONSENT MODULE. This is a sign-in
-- credential: it is minted by the door, it is spent to open a session, and its
-- lifetime rules are a sign-in's. The consent module owns the Privacy Policy
-- and the evidence; it deliberately knows nothing about Customer Sessions, and
-- the dependency runs one way — customers may depend on consent, never the
-- reverse (internal/consent's package doc). A table of half-finished sign-ins
-- belongs on the side that finishes them.
CREATE TABLE pending_consents (
    -- The token itself, opaque and high-entropy, generated exactly as a Customer
    -- Session token is and derived from nothing about the Customer. Primary key,
    -- so presenting it IS the lookup.
    id TEXT PRIMARY KEY,
    -- Who proved their address. The Customer record already exists by the time a
    -- row is written here: proof of ownership creates-or-reuses it and stamps
    -- verified_at, and only the SESSION is withheld. That ordering is why
    -- abandoning costs nothing and grants nothing — the person is verified and
    -- signed out, which is what they were before they started.
    --
    -- ON DELETE CASCADE, like `customer_sessions`: this is a convenience with a
    -- fifteen-minute life, not evidence. The evidence is in `consent_records`
    -- and is pointedly RESTRICTed.
    customer_id UUID NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    -- The address that was proven, as it was proven — copied rather than joined,
    -- for the same reason `consent_records.email` is: it is what the Consent
    -- Record this token leads to must assert, and it must not silently change
    -- underneath a half-finished sign-in.
    email TEXT NOT NULL,
    -- There is deliberately NO locale column here, although the sign-in that
    -- created the row knew one. The Mail Locale is written by the proof step
    -- itself — the create-or-reuse-and-stamp that runs before this row exists —
    -- so by the time a consent step is finished the language has already been
    -- remembered, and carrying a second copy across the step would be a second
    -- place for it to be true from.
    --
    -- Minutes, not days. The window only has to be long enough to read a short
    -- notice and tick a box; it is proof of email ownership in suspension, and
    -- the longer it lasts the longer a token pasted out of a shared machine's
    -- history is worth a session.
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Supports sweeping abandoned rows, and revoking a Customer's in-flight
-- attempts. Nothing sweeps today: rows are deleted on redemption, and an
-- abandoned one is unusable the moment it expires — the same posture
-- `customer_sessions` ships with.
CREATE INDEX pending_consents_expires_at_idx ON pending_consents (expires_at);
