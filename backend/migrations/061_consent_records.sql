-- Consent Records: the append-only evidence of what each person authorized
-- (#251, parent #249).
--
-- One row per CAPTURE ACT — not one row per Customer, and not one row per
-- consent. A person who signs in, accepts the Privacy Policy and declines
-- marketing writes exactly one row here; the same person turning marketing on
-- from their Customer Area three months later writes a second. The two rows
-- disagree, and that is the point: the log records what HAPPENED, and the
-- current-state columns on `customers` (migration 062) record what is TRUE NOW.
-- Every hot-path question — is this Customer gated? which boxes do we show? may
-- we send them a campaign? — is answered from the state, never from this table.
-- Nothing here is on a read path that a person waits for.
--
-- WHAT THE ROW SAYS HAPPENED IS NEVER ALTERED, AND NO ROW IS EVER DELETED. This
-- is the property the whole feature rests on, so it is worth being explicit
-- about what it forbids: no UPDATE to correct a mistyped answer (record the
-- correction as a new act), no DELETE to tidy up a test row in production, no
-- "fix the timestamp" after an incident. An evidence log that can be edited is
-- not evidence — a compliance officer holding a row from it must be able to say
-- that it is what the system observed at the time, and every write path that
-- could have altered it afterwards is a reason they cannot.
--
-- THE ONE RULE THAT ADMITS ANY POST-INSERT WRITE, stated as a rule rather than
-- as a list of exceptions, because it has already had to be widened once (#266)
-- and a special case that grows is a special case that was never the real
-- shape:
--
--   A column here may be written after insert only if it is NULL until some
--   LATER EVENT ABOUT THIS ACT occurs, is then written ONCE and never changed
--   or cleared, and CHANGES NOTHING THE ROW SAYS THE PERSON DID.
--
-- Two columns satisfy it and they are the only two: `confirmed_at`, which
-- records that the act was later corroborated from the address itself, and
-- `confirmation_sent_at` (migration 067), which records that its subject was
-- later told about it. Neither is an answer, a channel, a clock reading of the
-- act, or a piece of technical proof; each is an annotation saying what happened
-- NEXT, and the transcript underneath is byte-for-byte what it was at insert.
-- The rule's third clause is what makes that checkable rather than a promise: a
-- proposed column that would edit an answer, restate when the act occurred, or
-- revise the circumstances fails it outright, whatever it is called.
--
-- The alternative for both — a side table of annotations — buys strict
-- append-only at the cost of a join for questions ("was this tick ever
-- confirmed?", "was the titular told?") that are about one row and nothing else,
-- and it is not a trade this table makes. It is emphatically NOT a licence to
-- build a general log of sent mail on the back of the second stamp; see the
-- argument on that column in migration 067.
--
-- The rule is enforced in the shape of the repository's API rather than by a
-- database trigger: internal/consent/repository offers an insert, reads, and one
-- narrow single-purpose method per stamp, and there is no method that updates
-- anything else or deletes a row here. A trigger was considered and rejected —
-- it would refuse the legitimate stamps too, and buying immutability by making
-- the double opt-in impossible is the wrong trade.
--
-- A tick that changes nothing still writes a row (CONTEXT.md, "Consent
-- Record"): a guest who ticks marketing for an address they have not proven
-- changes no state at all (ADR 0035), and the record of them having ticked it
-- is exactly what the guidance's evidence template asks for.
CREATE TABLE consent_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Who this act was about. NOT NULL: every capture surface in the parent
    -- feature resolves a Customer before it records anything — sign-in creates
    -- or reuses the record from the proven email, and a checkout writes its
    -- Consent Record only when the sale commits, by which point the Customer
    -- exists. There is deliberately no such thing as a Consent Record floating
    -- free of a person; evidence about nobody proves nothing.
    --
    -- ON DELETE RESTRICT, unlike `customer_sessions` and the Follow tables,
    -- which cascade. Those hold conveniences that should evaporate with the
    -- account. This holds the proof that the account was lawfully processed,
    -- and the day somebody writes a Customer deletion it must be forced to
    -- decide what happens to this evidence rather than have the database
    -- silently answer "it disappears".
    customer_id UUID NOT NULL REFERENCES customers (id) ON DELETE RESTRICT,
    -- The email AS ASSERTED at the moment of capture, copied and never joined
    -- to. A denormalised duplicate of `customers.email` on purpose: the address
    -- typed into the box is part of what happened, and a Customer's stored email
    -- is a mutable current fact. If the two ever differ — a guest typing a
    -- stranger's address, an email corrected later — the row must still say what
    -- was actually typed.
    email TEXT NOT NULL,
    -- Which surface the person was on. An EXTENSIBLE SET, defined in full here
    -- although #251 writes only the first: `checkout` lands with the online
    -- checkout capture, `account_settings` and `unsubscribe_link` when the
    -- existing digest toggle and unsubscribe link start writing evidence, and
    -- `email_confirmation` when a Pending Confirmation is resolved by the link
    -- in a Sale Confirmation (ADR 0035). Naming them all now is cheap and makes
    -- the vocabulary one decision rather than four; a later channel is a
    -- migration that widens this CHECK. One since has: migration 067 adds
    -- `operator_request`, so the constraint below is no longer the live one.
    --
    -- CHECK rather than an ENUM type, following the house style every other
    -- constrained string column here uses (mail_locale, sale locale, payout
    -- request status): widening a CHECK is an ordinary ALTER in a forward-only
    -- migration, and it keeps the vocabulary readable in `psql` without
    -- consulting pg_type.
    channel TEXT NOT NULL
        CHECK (channel IN ('signin', 'checkout', 'account_settings', 'unsubscribe_link', 'email_confirmation')),
    -- The SERVER's clock, never the client's. A timestamp a browser could name
    -- is a timestamp an audit cannot use.
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- WHICH EDITION the person was shown. Resolved server-side from
    -- `policy_versions` at the moment of capture and never accepted from a
    -- request body: which policy somebody accepted is the platform's finding
    -- about the moment, not the client's assertion about it (see the note on
    -- service.PolicyView). NOT NULL, because a recorded acceptance that cannot
    -- say what was accepted is not evidence of anything.
    policy_version_id UUID NOT NULL REFERENCES policy_versions (id) ON DELETE RESTRICT,
    -- The three answers, each NULLABLE, and the null carries information: NULL
    -- means THE BOX WAS NOT SHOWN ON THIS SURFACE, which is a different fact
    -- from an explicit No. A Customer re-prompted after a version bump is shown
    -- the required box alone (their standing optional answers are not churned),
    -- so their row records TRUE, NULL, NULL — and reading those NULLs as
    -- refusals would turn a policy re-acceptance into a marketing opt-out.
    --
    -- BOOLEAN and not the tri-state the `customers` columns carry. A record
    -- says what the person DID — ticked or left unticked — while the state says
    -- what became true of them, and Pending Confirmation is a fact about the
    -- state, produced by combining a tick with `email_proven` below. Storing
    -- the state's vocabulary here would blur an answer with its consequence.
    policy_acceptance BOOLEAN,
    marketing_consent BOOLEAN,
    networking_consent BOOLEAN,
    -- Was this act behind Proof of Email Ownership? An EXPLICIT INPUT to the
    -- capture, never inferred from the channel: today `signin` is always proven
    -- and `checkout` may be either, but a surface's name is not a security
    -- property and the day one changes, an inference would silently start
    -- lying. This is the flag that decides whether an optional tick becomes
    -- `granted` or `pending_confirmation` (ADR 0035).
    email_proven BOOLEAN NOT NULL,
    -- When a Pending Confirmation this row created was later confirmed, for the
    -- double opt-in that lands with the Sale Confirmation's confirmation link.
    -- Null for every row #251 writes.
    --
    -- IT IS ONE OF THE TWO COLUMNS HERE EVER WRITTEN AFTER INSERT, under the
    -- one-way rule stated above: null to a timestamp, once, never back. It does
    -- not alter what the row says happened; it records that the act it describes
    -- was later CORROBORATED. Its sibling under the same rule is
    -- `confirmation_sent_at` (migration 067), which records that the act's
    -- subject was later TOLD — corroborated and answered being the only two
    -- things that may be said about an act after the fact without touching it.
    -- Each has its own narrow repository method naming its own column; neither
    -- is a general update.
    confirmed_at TIMESTAMPTZ,
    -- The technical proof the guidance's evidence template requires, where it is
    -- the row headed "Prueba técnica" — named here once so the column can be
    -- traced back to the document, and called technical proof everywhere else,
    -- because one concept gets one spelling (CONTEXT.md, Consent Record). The
    -- circumstances of the act, so that a record can be tied to a session and a
    -- device rather than only to an address.
    --
    -- All four NULLABLE, and none of them is a claim the platform vouches for.
    -- An IP behind a proxy chain, a user agent a browser is free to invent, an
    -- origin the page reported: they corroborate, they do not prove — what
    -- proves is `email_proven`. A capture surface that has none of them (a
    -- future server-side write, a job) records nulls rather than empty strings,
    -- because "not collected" and "collected as blank" are different answers.
    --
    -- `ip` is TEXT and not INET: it is whatever platform.ClientIP derived, and
    -- the evidence is what the platform observed, including the day it observes
    -- something malformed. A type that refuses to store the observation would
    -- lose it.
    --
    -- `session_id` is the identifier of the session the act happened under, as a
    -- plain string with no foreign key — deliberately, because the sessions it
    -- names are deleted at sign-out and expire on their own, and evidence must
    -- not be deletable by somebody pressing "sign out". At sign-in it is the
    -- Customer Session the capture MINTS, which is the only identifier that ties
    -- the record to what the person did next.
    ip TEXT,
    user_agent TEXT,
    session_id TEXT,
    origin_url TEXT
);

-- One index, on the question this table is actually asked: "show me everything
-- this person ever authorized", which is what a compliance request and a
-- support thread both come down to. Newest first, because that is the order
-- both are read in.
--
-- Nothing else is indexed. There is no index on `channel` or on
-- `policy_version_id`: a per-channel or per-edition report is a rare analytical
-- read over a small table, and an index maintained on every capture to serve a
-- query nobody runs weekly is a cost paid on the hot path for nothing.
CREATE INDEX idx_consent_records_customer ON consent_records (customer_id, captured_at DESC);
