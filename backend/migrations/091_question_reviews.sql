-- The Question Review: the Organization asks, the Platform Operator answers
-- (#406, parent #404, ADR 0056).
--
-- The unit of review is the question; the unit of the ask is the Event. An Org
-- Admin or Event Owner submits an Event's drafts and refused questions as ONE
-- Review, and the Operator answers it as one act but per question (#407). This
-- is the Payout Request's shape (ADR 0026) applied to a second ask: one
-- outstanding per Event, a snapshot of what was asked, a reason on a refusal,
-- and mail both ways.
--
-- THE STATE MACHINE:
--
--   outstanding  submitted and waiting on the Operator. At most ONE per Event,
--                held by the partial unique index below on the same terms as
--                payout_requests' — the INSERT that would make a second is
--                refused by the database, not by a read that raced it.
--   answered     the Operator gave every item a verdict (#407).
--   withdrawn    the Organization took it back while outstanding; its items go
--                back to draft.
--   lapsed       the Event started with it still outstanding (#410).
--
-- THE ACKNOWLEDGEMENT IS A RECORD, not a checkbox: acknowledged_at is NOT NULL
-- because a Review without one was never accepted, and it is stored beside
-- submitted_by so the platform can say who affirmed what the Organization was
-- choosing to collect, and when. It is where ADR 0045's "warned at authoring
-- time" stops being a banner.
--
-- answered_by/answered_at are the row's END, whichever way it ended: the
-- Operator's verdict, the Organization's withdrawal, or the lapse. One pair
-- rather than three, on the terms payout_requests' resolved_by/resolved_at are
-- — the status says which, and a reader wanting "who ended it" has one column
-- to look at. Both authorship columns are plain text, not references: they must
-- survive the account behind them.
CREATE TABLE question_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'outstanding'
        CHECK (status IN ('outstanding', 'answered', 'withdrawn', 'lapsed')),
    note TEXT,
    acknowledged_at TIMESTAMPTZ NOT NULL,
    submitted_by TEXT NOT NULL,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    answered_by TEXT,
    answered_at TIMESTAMPTZ,
    CONSTRAINT question_reviews_answer_signed
        CHECK ((answered_at IS NULL) = (answered_by IS NULL)),
    CONSTRAINT question_reviews_ended_is_answered
        CHECK ((status = 'outstanding') = (answered_at IS NULL))
);

-- One outstanding Review per Event.
CREATE UNIQUE INDEX question_reviews_one_outstanding_per_event
    ON question_reviews (event_id)
    WHERE status = 'outstanding';

CREATE INDEX question_reviews_event_submitted_idx
    ON question_reviews (event_id, submitted_at DESC);

-- What a Review carries: a question, or an Option of one. An item is one row
-- per thing the Operator reads and rules on, so a refusal over one question of
-- six names the one (ADR 0056). ticket_question_option_id is NULL for a
-- question item and set for an Option item; the Option's question travels
-- beside it in ticket_question_id so the two kinds list together.
--
-- verdict and reason are NULL until the Operator answers (#407): a verdict
-- against is `refused`, never rejected, and a refusal carries its reason.
CREATE TABLE question_review_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_review_id UUID NOT NULL REFERENCES question_reviews(id) ON DELETE CASCADE,
    ticket_question_id UUID NOT NULL REFERENCES ticket_questions(id) ON DELETE CASCADE,
    ticket_question_option_id UUID REFERENCES ticket_question_options(id) ON DELETE CASCADE,
    verdict TEXT CHECK (verdict IN ('approved', 'refused')),
    reason TEXT,
    CONSTRAINT question_review_items_refusal_has_reason
        CHECK (verdict IS DISTINCT FROM 'refused' OR reason IS NOT NULL)
);

-- A question, and an Option, rides a given Review once.
CREATE UNIQUE INDEX question_review_items_question_once
    ON question_review_items (question_review_id, ticket_question_id)
    WHERE ticket_question_option_id IS NULL;
CREATE UNIQUE INDEX question_review_items_option_once
    ON question_review_items (question_review_id, ticket_question_option_id)
    WHERE ticket_question_option_id IS NOT NULL;

CREATE INDEX question_review_items_review_idx
    ON question_review_items (question_review_id);
