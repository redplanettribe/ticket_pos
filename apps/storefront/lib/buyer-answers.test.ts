import assert from "node:assert/strict";
import { test } from "node:test";

import {
  hasAnythingToShow,
  heldRowFor,
  placeholderRowCount,
  saleOutstandingCount,
  withHeldRow,
  type BuyerTicket,
  type HeldTicket,
} from "./buyer-answers.ts";
import type { Question, QuestionAnswer } from "./ticket-questions.ts";

/**
 * The buyer's own surface, as rules (#315, ADR 0044; narrowed by #344,
 * ADR 0049).
 *
 * These assert the decisions that change what a person SEES: which sale-scoped
 * row gets a question panel, where its questions come from, and the two "draw
 * nothing at all" rules that keep this feature invisible to the great majority
 * of purchases. They deliberately do not re-test what an Outstanding Answer is:
 * that is decided on the server, in one place (#313).
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
  return {
    ticket_id: "t1",
    ordinal: 1,
    ticket_type_name: "General",
    assignment_state: "unassigned",
    assignable: true,
    ...overrides,
  };
}

function held(overrides: Partial<HeldTicket> = {}): HeldTicket {
  const pair: QuestionAnswer = { question: question(), answer: null };
  return {
    ticket_id: "t1",
    event_name: "Fest",
    event_slug: "fest",
    ticket_type_name: "General",
    answerable: true,
    answerable_refusal: "",
    outstanding_count: 1,
    questions: [pair],
    ...overrides,
  };
}

// ONLY THE HOLDER ANSWERS (ADR 0049). A row the buyer does not hold gets no
// held row — whatever the held list happens to contain — and so draws no
// question. The join is on the id, not on the flag alone.
test("a ticket the buyer does not hold has no held row, even when the held list names it", () => {
  assert.equal(heldRowFor(ticket({ self_held: false }), [held()]), null);
  assert.equal(heldRowFor(ticket({ self_held: undefined }), [held()]), null);
});

test("the self-held ticket finds its questions in the held list by id", () => {
  const own = held({ ticket_id: "t1" });
  assert.equal(heldRowFor(ticket({ self_held: true }), [held({ ticket_id: "other" }), own]), own);
});

// A BUYER WHO GAVE THEIR TICKET AWAY sees it as an ordinary assignable row.
// Between the two fetches the sale-scoped row may still say self_held while
// the held list no longer carries it; a panel with no questions would be
// wrong, and an assignment row is right either way.
test("a self-held ticket missing from the held list draws as an ordinary row", () => {
  assert.equal(heldRowFor(ticket({ self_held: true }), []), null);
  assert.equal(heldRowFor(ticket({ self_held: true }), [held({ ticket_id: "other" })]), null);
});

// THE RULE THAT KEEPS THIS FEATURE INVISIBLE. Most Organizations have never
// written a Ticket Question, so most Customer Areas must look precisely as they
// did before this shipped.
test("a sale whose held ticket asks nothing draws no question half", () => {
  assert.equal(hasAnythingToShow([ticket({ self_held: true })], [held({ questions: [] })]), false);
  assert.equal(hasAnythingToShow([], []), false);
});

// And a Ticket Type that asks plenty is still nobody's question here when the
// buyer holds none of the sale's Tickets: those are their Holders' to answer.
test("a sale where the buyer holds nothing draws no question half however much is asked", () => {
  assert.equal(hasAnythingToShow([ticket({ self_held: false })], [held()]), false);
});

test("the self-held ticket asking something is enough", () => {
  assert.equal(
    hasAnythingToShow([ticket({ ticket_id: "a" }), ticket({ ticket_id: "b", self_held: true })], [
      held({ ticket_id: "b" }),
    ]),
    true,
  );
});

// The count is READ from the held row and never recomputed, and a Ticket the
// buyer does not hold owes THEM nothing, whatever it owes its Holder.
test("the sale's outstanding count is the held ticket's, and nothing for the rest", () => {
  const tickets = [ticket({ ticket_id: "a", self_held: true }), ticket({ ticket_id: "b" })];
  const rows = [held({ ticket_id: "a", outstanding_count: 3 }), held({ ticket_id: "b", outstanding_count: 5 })];
  assert.equal(saleOutstandingCount(tickets, rows), 3);
  assert.equal(saleOutstandingCount([ticket({ ticket_id: "b" })], rows), 0);
});

// The held write returns ONE Ticket; the page patches it in by id so the badge
// above and the row that changed cannot disagree.
test("a written held ticket replaces its row and never duplicates it", () => {
  const before = [held({ ticket_id: "a", outstanding_count: 1 }), held({ ticket_id: "b" })];
  const after = withHeldRow(before, held({ ticket_id: "a", outstanding_count: 0 }));
  assert.equal(after.length, 2);
  assert.equal(after[0]?.outstanding_count, 0);
  assert.equal(after[1], before[1]);
  assert.equal(withHeldRow([], held({ ticket_id: "c" })).length, 1);
});

test("the skeleton reserves one row per ticket, at least one and at most twelve", () => {
  assert.equal(placeholderRowCount(4), 4);
  assert.equal(placeholderRowCount(0), 1);
  assert.equal(placeholderRowCount(-3), 1);
  assert.equal(placeholderRowCount(2.7), 2);
  assert.equal(placeholderRowCount(40), 12);
  assert.equal(placeholderRowCount(Number.NaN), 1);
});
