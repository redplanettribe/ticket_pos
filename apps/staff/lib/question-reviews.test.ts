import assert from "node:assert/strict";
import test from "node:test";

import {
  canSubmitReview,
  eventStarted,
  isOutstanding,
  questionCount,
  reviewableQuestions,
  type QuestionReview,
} from "./question-reviews.ts";
import type { TicketQuestion, TicketQuestionReviewStatus } from "./ticket-questions.ts";

function question(id: string, review_status: TicketQuestionReviewStatus, retired = false): TicketQuestion {
  return {
    id,
    label: id,
    kind: "short_text",
    required: false,
    timing: "at_checkout",
    sort_order: 0,
    retired,
    review_status,
    options: [],
  };
}

function review(status: QuestionReview["status"]): QuestionReview {
  return {
    id: "r1",
    event_id: "e1",
    status,
    note: null,
    acknowledged_at: "2026-08-23T10:00:00Z",
    submitted_by: "admin@example.com",
    submitted_at: "2026-08-23T10:00:00Z",
    answered_by: null,
    answered_at: null,
    items: [
      { id: "i1", ticket_question_id: "q1" },
      { id: "i2", ticket_question_id: "q2" },
      { id: "i3", ticket_question_id: "q2", ticket_question_option_id: "o1" },
    ],
  };
}

const NOW = new Date("2026-08-23T12:00:00Z");

test("only an outstanding Review is outstanding", () => {
  assert.equal(isOutstanding(review("outstanding")), true);
  assert.equal(isOutstanding(review("withdrawn")), false);
  assert.equal(isOutstanding(null), false);
});

test("an Event has started at and after its start, and never without one", () => {
  assert.equal(eventStarted("2026-08-23T12:00:00Z", NOW), true);
  assert.equal(eventStarted("2026-08-23T12:00:01Z", NOW), false);
  assert.equal(eventStarted(null, NOW), false);
});

test("a submission carries live drafts and refused questions only", () => {
  const carried = reviewableQuestions([
    question("draft", "draft"),
    question("refused", "refused"),
    question("approved", "approved"),
    question("under", "under_review"),
    question("retired-draft", "draft", true),
  ]).map((q) => q.id);
  assert.deepEqual(carried, ["draft", "refused"]);
});

test("submit is offered only before the start, with nothing outstanding and something to carry", () => {
  const drafts = [question("q1", "draft")];
  assert.equal(canSubmitReview(drafts, null, "2026-09-01T00:00:00Z", NOW), true);
  assert.equal(canSubmitReview(drafts, review("withdrawn"), "2026-09-01T00:00:00Z", NOW), true);
  assert.equal(canSubmitReview(drafts, review("outstanding"), "2026-09-01T00:00:00Z", NOW), false);
  assert.equal(canSubmitReview(drafts, null, "2026-08-01T00:00:00Z", NOW), false);
  assert.equal(canSubmitReview([question("q1", "approved")], null, null, NOW), false);
});

test("a Review's question count leaves its Options out", () => {
  assert.equal(questionCount(review("outstanding")), 2);
});
