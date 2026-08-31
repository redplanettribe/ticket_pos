-- Terms acceptance on the Customer platform (#536, parent #533, ADR 0066): the
-- attendee-capacity acceptance, riding the consent machinery migrations 061 and
-- 062 built rather than growing a parallel one.
--
-- The division of labour is exactly theirs. `consent_records` stays the
-- append-only log of capture ACTS and gains the Terms answer as one more
-- nullable column pair; `customers` stays the current state and gains the
-- "which edition has this person accepted?" pair the sign-in gate reads. One
-- capture act that includes the Terms box writes one row and one stamp — there
-- is no second evidence log and no second write path.

-- THE ANSWER, on the evidence log.
--
-- Nullable, and the null carries the same information the three answer columns
-- beside it carry: NULL means THE BOX WAS NOT SHOWN ON THIS SURFACE, which is a
-- different fact from a refusal. A capture that asked only about the Privacy
-- Policy — or an account-settings toggle, an unsubscribe, an operator-recorded
-- withdrawal — says nothing about the Terms and records nothing about them.
--
-- `terms_version_id` is WHICH EDITION the person was shown, resolved
-- server-side from `terms_versions` at the moment of capture and never accepted
-- from a request body — the same finding-not-assertion rule `policy_version_id`
-- states. It differs from that column in being NULLABLE, because unlike the
-- Privacy Policy the Terms are not named on every act: the edition is part of
-- the answer's evidence, so it travels with the answer or not at all, which is
-- what the CHECK below pins.
ALTER TABLE consent_records ADD COLUMN terms_acceptance BOOLEAN;
ALTER TABLE consent_records ADD COLUMN terms_version_id UUID REFERENCES terms_versions (id) ON DELETE RESTRICT;

-- An answer with no edition names nothing; an edition with no answer records no
-- act. The two halves travel together or not at all.
ALTER TABLE consent_records ADD CONSTRAINT consent_records_terms_answer_check
    CHECK ((terms_acceptance IS NULL) = (terms_version_id IS NULL));

-- THE STATE, on the Customer: when, and of WHICH EDITION.
--
-- Two columns and not one, for migration 062's reason verbatim: acceptance is
-- of a VERSION and never of "the Terms" in the abstract, so the gate's question
-- is "have they accepted the edition current NOW". Publishing a new
-- `terms_versions` row re-gates the entire Customer base without touching a row
-- here — the stored id simply stops matching — which is the one-time re-gate
-- migration 105's seed performs on the day this deploys (#533 §34).
--
-- Both NULLABLE and null on every existing row, deliberately: nobody has
-- accepted the Terms yet, there is nothing truthful to backfill with, and a
-- DEFAULT would manufacture evidence of an act that never took place.
--
-- There is deliberately NO withdrawal path and no column for one. Terms
-- acceptance is contractual (T&C §2, ADR 0066): Withdraw All and every
-- consent-withdrawal channel leave these columns untouched, and the only thing
-- that ever unsettles them is a later edition owing re-acceptance.
ALTER TABLE customers ADD COLUMN terms_accepted_at TIMESTAMPTZ;
ALTER TABLE customers ADD COLUMN terms_version_id UUID REFERENCES terms_versions (id) ON DELETE RESTRICT;

ALTER TABLE customers ADD CONSTRAINT customers_terms_acceptance_check
    CHECK ((terms_accepted_at IS NULL) = (terms_version_id IS NULL));

-- No new index. The state pair is read by primary key with the Customer row the
-- sign-in already loads, and the evidence columns are written once and read by
-- compliance queries over a small table — the same verdicts 061 and 062 reach.
