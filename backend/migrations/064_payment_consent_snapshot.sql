-- The consent answers, snapshotted on the Payment so they survive the Payment
-- Provider redirect (#253, parent #249).
--
-- This is the fourth time this table has needed exactly this, and the reason has
-- not changed since migration 027: an Online Sale is COMMITTED BY THE CONFIRM
-- LEG, which runs on the provider's return redirect and carries back a
-- transaction id and nothing else. The Tax ID (027), the phone (029), the
-- Affiliate Link (036) and the Sale Locale (059) are all here because a buyer
-- fact not written down at begin-checkout is a buyer fact lost by the time there
-- is a sale to write it on. The consent boxes are answered on the same dialog,
-- in the same request, and would be lost the same way.
--
-- WHY NOT WRITE THE CONSENT RECORD AT BEGIN INSTEAD. Because a Consent Record is
-- evidence that a transaction happened under an informed person, and an
-- abandoned checkout is not a transaction. `consent_records.customer_id` is NOT
-- NULL and the Customer itself is upserted only when the sale commits, so there
-- is not even a row to point at until then — the same reason a declined Payment
-- leaves no Customer leaves no evidence. These columns are therefore a HELD
-- ANSWER, not a record of one: they are read once, inside
-- ApprovePaymentAndCommitSale's transaction, and turned into the immutable row
-- there, beside the Customer upsert and the Ticket Sale (parent spec: "abandoned
-- checkouts record no consent").
--
-- THE THREE ANSWERS ARE NULLABLE BOOLEANS, and all three states are load-bearing
-- exactly as they are on `consent_records`:
--
--   true  — the box was shown and ticked
--   false — the box was shown and left unticked, which is an explicit No
--           (ADR 0034), never silence
--   NULL  — the box WAS NOT SHOWN on this surface, which is not a No
--
-- The guest path this ticket builds always shows all three, so it writes three
-- non-null values. NULL is here for #254: a signed-in Customer is shown only the
-- boxes they have not answered, and reading their unshown boxes as refusals
-- would turn a purchase into a marketing opt-out.
--
-- No CHECK requiring policy_acceptance to be true. The service refuses a
-- checkout without it (consent.ErrPolicyAcceptanceRequired) and so nothing can
-- currently store `false` here, but the constraint would be stating a rule about
-- a MOMENT — "no checkout may begin unaccepted" — as a rule about a row, and the
-- day a surface legitimately records a No (a re-prompt someone declined, say)
-- the constraint would refuse the honest record rather than the dishonest one.
ALTER TABLE payments ADD COLUMN consent_policy_acceptance BOOLEAN NULL;
ALTER TABLE payments ADD COLUMN consent_marketing BOOLEAN NULL;
ALTER TABLE payments ADD COLUMN consent_networking BOOLEAN NULL;

-- The technical proof of the capture act, held the same way and for the same
-- reason: it describes the request the buyer answered the boxes IN, and the
-- confirm request is a different one — a redirect back from a third party,
-- possibly minutes later, possibly from another network. Deriving the evidence
-- at confirm would record the circumstances of the provider's return rather than
-- of the consent, which is worse than recording none.
--
-- Nullable, and NULL means "not collected" rather than "collected as blank" —
-- the distinction migration 061 draws for the same four fields. Every value here
-- comes from the REQUEST (platform.ClientIP, the User-Agent and Referer headers
-- as the Storefront BFF relayed them) and never from the request body, which a
-- client composes and could therefore make say anything.
--
-- There is no session id column, and its absence is a fact rather than an
-- omission: this is the GUEST checkout, where by definition there is no Customer
-- Session, and where the act spans a provider redirect that no session outlives.
-- What ties a checkout's evidence together is the Payment and the Ticket Sale it
-- became.
ALTER TABLE payments ADD COLUMN consent_ip TEXT NULL;
ALTER TABLE payments ADD COLUMN consent_user_agent TEXT NULL;
ALTER TABLE payments ADD COLUMN consent_origin_url TEXT NULL;

-- No index on any of them. They are read exactly once, by primary key, in the
-- transaction that settles their own Payment; nothing selects on them, and
-- nothing ever will — the question "who consented to what" is answered from
-- `consent_records` and `customers`, which is the whole point of writing the
-- record at commit rather than leaving the answer here.
--
-- Every Payment recorded before this migration keeps three NULL answers, which
-- reads correctly as "these boxes were not shown", because they were not: the
-- dialog had no consent section. Those Payments can still confirm — a checkout
-- begun before the deploy must not fail at the till — and the commit path writes
-- no Consent Record for them, because there is no capture act to evidence.
