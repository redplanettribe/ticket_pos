import assert from "node:assert/strict";
import { test } from "node:test";

import {
  hasAnswerLink,
  hasAnythingToShow,
  saleHasOutstandingAnswers,
  saleOutstandingCount,
  type BuyerTicket,
} from "./buyer-answers.ts";
import type { Question, QuestionAnswer } from "./answer-link.ts";

/**
 * The buyer's own surface, as rules (#315, ADR 0044).
 *
 * These assert the decisions that change what a person SEES, and deliberately
 * do not re-test what an Outstanding Answer is: that is decided on the server,
 * in one place (#313), and a copy of the rule here would be a third opinion
 * about a debt that already has two on purpose. What is tested is that this
 * side reads the server's number rather than recomputing it, and the two
 * "draw nothing at all" rules, which are what keep this feature invisible to the
 * great majority of purchases.
 */

function question(overrides: Partial<Question> = {}): Question {
  return {
    id: "q1",
    label: "T-shirt size",
    kind: "short_text",
    required: true,
    sort_order: 0,
    retired: false,
    options: [],
    ...overrides,
  };
}

function ticket(overrides: Partial<BuyerTicket> = {}): BuyerTicket {
  const pair: QuestionAnswer = { question: question(), answer: null };
  return {
    ticket_id: "t1",
    ordinal: 1,
    ticket_type_name: "General",
    answer_link: "https://example.test/answer?token=x",
    answerable: true,
    answerable_refusal: "",
    outstanding_count: 1,
    questions: [pair],
    ...overrides,
  };
}

test("a sale owing nothing says so, and one owing something says so", () => {
  assert.equal(saleHasOutstandingAnswers([ticket({ outstanding_count: 0 })]), false);
  assert.equal(saleHasOutstandingAnswers([ticket({ outstanding_count: 1 })]), true);
});

// DEBTS AND NOT TICKETS, so a Ticket owing three counts three. The same unit the
// Organization's own headline figure uses, so a buyer on the phone and the
// member of staff they are talking to are counting the same things.
test("the sale's count sums debts across its tickets rather than counting tickets", () => {
  const tickets = [
    ticket({ ticket_id: "a", outstanding_count: 3 }),
    ticket({ ticket_id: "b", outstanding_count: 1 }),
    ticket({ ticket_id: "c", outstanding_count: 0 }),
  ];
  assert.equal(saleOutstandingCount(tickets), 4);
  assert.equal(saleHasOutstandingAnswers(tickets), true);
});

// The count is READ and never recomputed. A browser that re-derived the debt
// from the questions beside it would be the third statement of a rule that
// already lives twice on purpose — and it would disagree first on the
// retired-question case, which nobody thinks about.
test("the count is taken from the server even when the questions look answered", () => {
  const answered: QuestionAnswer = {
    question: question(),
    answer: { text: "M", number: null, date: null, checked: null, options: [], updated_at: "" },
  };
  // Every question has a reply, and the server still says one thing is owed.
  // This side reports what it was told.
  assert.equal(saleOutstandingCount([ticket({ questions: [answered], outstanding_count: 1 })]), 1);
});

// A COPY BUTTON IS A PROMISE. The API withholds the link once the Ticket can no
// longer be answered — a started Event, a reversed Sale — and this is the check
// that keeps a dead link from being offered. Somebody who pastes one into a
// group chat has finished the task as far as they know.
test("a ticket with no link offers nothing to copy, whatever else it says", () => {
  assert.equal(hasAnswerLink(ticket()), true);
  assert.equal(hasAnswerLink(ticket({ answer_link: "", answerable: false })), false);
});

// A Ticket whose questions are all answered KEEPS its link. The holder may
// correct what the buyer guessed, which is the whole point of ADR 0044 — so
// this must not be tied to the outstanding count.
test("a fully answered ticket still offers its link", () => {
  assert.equal(hasAnswerLink(ticket({ outstanding_count: 0 })), true);
});

// THE RULE THAT KEEPS THIS FEATURE INVISIBLE. Most Organizations have never
// written a Ticket Question, so most Customer Areas must look precisely as they
// did before this shipped. An empty "Questions about these tickets" heading on
// every purchase anybody ever made would be the feature announcing itself to
// the people it has nothing to say to.
test("a sale whose ticket types ask nothing draws no section at all", () => {
  assert.equal(hasAnythingToShow([ticket({ questions: [] })]), false);
  assert.equal(hasAnythingToShow([]), false);
});

// One Ticket Type asking something is enough, even when another asks nothing —
// a sale can carry both, and the section belongs to the sale.
test("a sale draws the section when any one of its tickets is asked something", () => {
  assert.equal(
    hasAnythingToShow([ticket({ ticket_id: "a", questions: [] }), ticket({ ticket_id: "b" })]),
    true,
  );
});
