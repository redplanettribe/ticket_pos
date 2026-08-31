-- The Terms answer, snapshotted on the Payment so it survives the Payment
-- Provider redirect (#537, parent #533, ADR 0066) — the fifth arrival on the
-- reason migration 064 wrote down: an Online Sale is committed by the confirm
-- leg, which carries back a transaction id and nothing else, so a buyer fact
-- not written down at begin-checkout is a buyer fact lost by the time there is
-- a sale to write it on. The Terms box is answered on the same dialog, in the
-- same request, as the three consent boxes already held here.
--
-- HELD, NOT RECORDED, exactly as its three neighbours are: read once, inside
-- ApprovePaymentAndCommitSale's transaction, and turned into the immutable
-- Consent Record there. An abandoned checkout evidences nothing.
--
-- The same nullable-boolean states are load-bearing:
--
--   true  — the box was shown and ticked
--   false — the box was shown and left unticked, which the service refuses at
--           begin today (consent.ErrTermsAcceptanceRequired) — the absence of
--           a CHECK is migration 064's ruling restated: the refusal is about a
--           MOMENT, not about a row
--   NULL  — the box WAS NOT SHOWN on this dialog, which is the ordinary case:
--           a Customer met the Terms at sign-in (#536) and owes nothing here
ALTER TABLE payments ADD COLUMN consent_terms_acceptance BOOLEAN NULL;

-- WHICH EDITION the buyer was shown, resolved server-side at begin-checkout —
-- and this column is why the Terms hold is a PAIR where the policy hold is a
-- single boolean. The commit leg runs minutes later, on the provider's
-- schedule, and a Terms edition published between the two legs must not let
-- the record claim an acceptance of a text nobody was shown (#533 §34: the
-- evidence names the version). The held edition is what the capture writes,
-- onto the record and onto the Customer's state alike — so a buyer who
-- accepted the superseded edition mid-bump is, correctly, still owed the new
-- one at their next sign-in.
--
-- The Privacy Policy hold beside it deliberately keeps its shape: its version
-- has always resolved at capture, and rewriting settled semantics is not this
-- migration's business.
ALTER TABLE payments ADD COLUMN consent_terms_version_id UUID NULL REFERENCES terms_versions (id) ON DELETE RESTRICT;

-- An answer with no edition names nothing; an edition with no answer holds no
-- act — migration 106's rule, applied to the hold as well as the record.
ALTER TABLE payments ADD CONSTRAINT payments_terms_hold_check
    CHECK ((consent_terms_acceptance IS NULL) = (consent_terms_version_id IS NULL));

-- No index, for migration 064's reason verbatim: read exactly once, by primary
-- key, in the transaction that settles their own Payment. Every Payment
-- recorded before this migration keeps a NULL pair, which reads correctly as
-- "this dialog drew no Terms box", because it did not.
