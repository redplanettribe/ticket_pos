-- A Ticket Question, and an Option, is born a draft and is asked of nobody until
-- a Platform Operator has approved it (#405, parent #404, ADR 0056).
--
-- ADR 0045 kept the whole feature dark until the Privacy Policy describes the
-- collection. This is the second lock, not a replacement for it: once the flag
-- flips, the Operator still reads each question before it is asked, because
-- nothing else stops "any medical conditions?" going into a free-text box on a
-- Tuesday with the platform as its processor by Wednesday.
--
-- THE STATE MACHINE, on both tables:
--
--   draft         authored, asked of nobody. THE DEFAULT, and the only value
--                 a column default ever produces.
--   under_review  carried by an outstanding Question Review (#406).
--   approved      the ONLY state in which the row is asked at checkout, offered
--                 on the public Event page, answered in the Customer Area,
--                 chased by the Answer Reminder, counted as an Outstanding
--                 Answer, shown on the Holder List or given a column in the
--                 Sales Export.
--   refused       with the Operator's reason, from where it is edited back into
--                 a draft.
--
-- APPROVAL IS NEVER A COLUMN DEFAULT. It is the record of a review: who
-- approved, and when. The one exception is the grandfathering backfill that
-- follows (090), which approves the rows already in production and says so in
-- approved_by — a question can be born approved by that migration and by
-- nothing else.
--
-- A REVOCATION IS A RETIREMENT with a reason (ADR 0056): revoked_at is written
-- beside retired_at, review_status stays `approved` because the approval was
-- real and the Answers given under it stay, and the shared "asked" predicate
-- (`approved` AND NOT retired) stops asking it from that moment. The
-- authorship columns are plain text and not references, on the terms
-- payout_requests' are: they record who did it, and they must survive the
-- account behind them.
ALTER TABLE ticket_questions
    ADD COLUMN review_status TEXT NOT NULL DEFAULT 'draft'
        CHECK (review_status IN ('draft', 'under_review', 'approved', 'refused')),
    ADD COLUMN approved_at TIMESTAMPTZ,
    ADD COLUMN approved_by TEXT,
    ADD COLUMN refused_at TIMESTAMPTZ,
    ADD COLUMN refused_by TEXT,
    ADD COLUMN refusal_reason TEXT,
    ADD COLUMN revoked_at TIMESTAMPTZ,
    ADD COLUMN revoked_by TEXT,
    ADD COLUMN revocation_reason TEXT;

-- An approval carries its author, a refusal its author and its reason, a
-- Revocation both: a verdict nobody signed is not a record of a review.
ALTER TABLE ticket_questions
    ADD CONSTRAINT ticket_questions_approval_signed
        CHECK ((approved_at IS NULL) = (approved_by IS NULL)),
    ADD CONSTRAINT ticket_questions_refusal_signed
        CHECK ((refused_at IS NULL) = (refused_by IS NULL) AND (refused_at IS NULL) = (refusal_reason IS NULL)),
    ADD CONSTRAINT ticket_questions_revocation_signed
        CHECK ((revoked_at IS NULL) = (revoked_by IS NULL) AND (revoked_at IS NULL) = (revocation_reason IS NULL)),
    ADD CONSTRAINT ticket_questions_approved_is_signed
        CHECK (review_status <> 'approved' OR approved_at IS NOT NULL);

-- The same on Options, because an Option can change what a question means: a
-- dietary list that gains "coeliac" has become a health question. An Option
-- added to an approved question is a draft Option until a Question Review
-- approves it, and the question keeps collecting in its approved shape.
ALTER TABLE ticket_question_options
    ADD COLUMN review_status TEXT NOT NULL DEFAULT 'draft'
        CHECK (review_status IN ('draft', 'under_review', 'approved', 'refused')),
    ADD COLUMN approved_at TIMESTAMPTZ,
    ADD COLUMN approved_by TEXT,
    ADD COLUMN refused_at TIMESTAMPTZ,
    ADD COLUMN refused_by TEXT,
    ADD COLUMN refusal_reason TEXT,
    ADD COLUMN revoked_at TIMESTAMPTZ,
    ADD COLUMN revoked_by TEXT,
    ADD COLUMN revocation_reason TEXT;

ALTER TABLE ticket_question_options
    ADD CONSTRAINT ticket_question_options_approval_signed
        CHECK ((approved_at IS NULL) = (approved_by IS NULL)),
    ADD CONSTRAINT ticket_question_options_refusal_signed
        CHECK ((refused_at IS NULL) = (refused_by IS NULL) AND (refused_at IS NULL) = (refusal_reason IS NULL)),
    ADD CONSTRAINT ticket_question_options_revocation_signed
        CHECK ((revoked_at IS NULL) = (revoked_by IS NULL) AND (revoked_at IS NULL) = (revocation_reason IS NULL)),
    ADD CONSTRAINT ticket_question_options_approved_is_signed
        CHECK (review_status <> 'approved' OR approved_at IS NOT NULL);
