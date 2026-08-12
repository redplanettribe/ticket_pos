-- Current consent state on the Customer: what is TRUE NOW (#251, parent #249).
--
-- The companion to migration 061, and the division of labour between them is
-- the whole design. `consent_records` is the append-only log of capture ACTS —
-- what happened, in order, with the proof of the circumstances. These five
-- columns are the current answer to the two questions the running system asks
-- constantly: is this Customer gated, and which boxes do we show them? Every
-- hot-path read goes here. Nothing on a request path aggregates the log,
-- because a current fact reconstructed by scanning history is a current fact
-- that gets slower and eventually gets it wrong.
--
-- They are on `customers` rather than in a table of their own for the same
-- reason `digest_enabled` and `mail_locale` are: they are single-valued facts
-- about one person, read with every Customer row, and a side table would add a
-- join to every sign-in to hold at most one row.

-- POLICY ACCEPTANCE: when, and of WHICH EDITION.
--
-- Two columns and not one, because Policy Acceptance is of a VERSION and never
-- of "the policy" in the abstract (CONTEXT.md). A timestamp alone would answer
-- "have they ever accepted?" and the gate does not ask that: it asks whether
-- they have accepted the edition that is current NOW. Publishing a new
-- `policy_versions` row therefore re-gates the entire customer base without
-- touching a single row here — the stored version id simply stops matching the
-- current one, which is exactly the blast radius migration 060 warns about.
--
-- Both NULLABLE, and null is the ordinary state of most rows on the day this
-- ships: every Customer a box office sale, a Sale Import or a pre-consent
-- checkout ever created has no acceptance and gets one the first time they
-- themselves act on a Storefront surface. There is deliberately no backfill and
-- no default — staff never accept on a buyer's behalf, so there is nothing
-- truthful to backfill WITH, and a DEFAULT NOW() here would manufacture
-- evidence of an act that never took place.
ALTER TABLE customers ADD COLUMN policy_accepted_at TIMESTAMPTZ;
ALTER TABLE customers ADD COLUMN policy_version_id UUID REFERENCES policy_versions (id) ON DELETE RESTRICT;

-- The two halves of one fact travel together or not at all. An acceptance with
-- no edition names nothing; an edition with no timestamp records no act.
ALTER TABLE customers ADD CONSTRAINT customers_policy_acceptance_check
    CHECK ((policy_accepted_at IS NULL) = (policy_version_id IS NULL));

-- THE TWO OPTIONAL CONSENTS, tri-state plus null.
--
-- Four values, and each one is a different sentence:
--
--   NULL                    they have never been asked, or never answered
--   'granted'               they said yes, with the email proven
--   'denied'                they said no — explicitly, including by leaving the
--                           unticked box unticked. Silence at a capture moment
--                           is a No and is recorded as one (ADR 0034).
--   'pending_confirmation'  somebody ticked it who had not proven the address
--                           (ADR 0035): DENIED FOR SENDING, UNANSWERED FOR
--                           PROMPTING. It never expires and no job sweeps it;
--                           it resolves when the proven owner answers, or when
--                           they click the confirmation link in their Sale
--                           Confirmation.
--
-- The two prompting rules that fall out of that, and that every capture surface
-- shares: a box is shown when its state is NULL or 'pending_confirmation', and
-- a box is never pre-ticked from stored state — consent is affirmative, so what
-- is stored decides whether to ASK, never what to show as already agreed.
--
-- TEXT with a CHECK, the house style for a constrained vocabulary
-- (customers.mail_locale, ticket_sales.locale, payout_requests.status), for the
-- reasons migration 061 gives on `channel`.
--
-- NULLABLE rather than NOT NULL DEFAULT 'denied', which was considered and is
-- wrong: it would record every Customer who has ever existed as having refused
-- marketing, which is a lie of exactly the kind this feature exists to stop
-- telling. It also erases the transition rule in ADR 0034 — the Follow Digest
-- keeps going to Customers who have NOT YET ANSWERED on the strength of the
-- Follow they pressed, and "not yet answered" has to be a storable state for
-- that sentence to be implementable.
ALTER TABLE customers ADD COLUMN marketing_consent TEXT
    CHECK (marketing_consent IN ('granted', 'denied', 'pending_confirmation'));
ALTER TABLE customers ADD COLUMN networking_consent TEXT
    CHECK (networking_consent IN ('granted', 'denied', 'pending_confirmation'));

-- `digest_enabled` is NOT replaced by `marketing_consent`, and this is the one
-- piece of apparent redundancy in the schema that is deliberate (ADR 0034).
-- They are one switch with two columns: Marketing Consent is the LAWFUL BASIS
-- and `digest_enabled` remains the OPERATIONAL flag, written in lockstep by the
-- consent-write service — granted turns it on, denied turns it off, and a
-- Pending Confirmation leaves it exactly as it was, because a stranger's tick
-- must not enable mail and a stranger's tick must not silence a legacy
-- subscriber either. What keeps `digest_enabled` from being an independent
-- source of truth is that nothing writes it except that one service and the
-- Customer's own toggle, which is itself a capture act.
--
-- Collapsing them would have meant either losing the legacy default's meaning —
-- `digest_enabled = true` on every Customer ever created is NOT consent and is
-- never claimed as such — or migrating it into 'granted', which would fabricate
-- exactly the evidence this table is built to avoid fabricating.

-- No index on any of these. They are read one row at a time, by primary key or
-- by the unique email, as part of loading the Customer who is signing in. A
-- report over "how many Customers granted marketing" is a rare full scan of a
-- small table and does not justify an index maintained on every capture.
