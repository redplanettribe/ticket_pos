import assert from "node:assert/strict";
import test from "node:test";

import {
  MAX_TICKET_QUESTION_LABEL_LENGTH,
  MAX_TICKET_QUESTION_OPTIONS,
  TICKET_QUESTION_KINDS,
  TICKET_QUESTION_KIND_KEYS,
  canAddOption,
  canRetireOption,
  isAsked,
  isValidLabel,
  liveOptions,
  liveQuestions,
  moveQuestion,
  offersOptions,
  retiredOptions,
  retiredQuestions,
  reviewReason,
  type TicketQuestion,
  type TicketQuestionKind,
  type TicketQuestionOption,
} from "./ticket-questions.ts";

function option(id: string, label: string, retired = false): TicketQuestionOption {
  return { id, label, sort_order: 0, retired, review_status: "approved" };
}

function question(
  id: string,
  kind: TicketQuestionKind,
  options: TicketQuestionOption[] = [],
  retired = false,
): TicketQuestion {
  return {
    id,
    label: `Question ${id}`,
    kind,
    required: false,
    timing: "at_checkout",
    sort_order: 0,
    retired,
    review_status: "approved",
    options,
  };
}

test("the editor offers exactly the seven kinds, and names every one of them", () => {
  assert.equal(TICKET_QUESTION_KINDS.length, 7);
  for (const kind of TICKET_QUESTION_KINDS) {
    assert.ok(TICKET_QUESTION_KIND_KEYS[kind], `${kind} has no catalog key`);
  }
});

test("only single choice and multiple choice are answered by choosing", () => {
  const expected: Record<TicketQuestionKind, boolean> = {
    short_text: false,
    long_text: false,
    single_choice: true,
    multi_choice: true,
    number: false,
    date: false,
    checkbox: false,
  };
  for (const kind of TICKET_QUESTION_KINDS) {
    assert.equal(offersOptions(kind), expected[kind], kind);
  }
});

test("a retired Option leaves the live list without leaving the question", () => {
  const q = question("q1", "single_choice", [
    option("o1", "S"),
    option("o2", "Medium", true),
    option("o3", "L"),
  ]);
  assert.deepEqual(
    liveOptions(q).map((o) => o.id),
    ["o1", "o3"],
  );
  assert.deepEqual(
    retiredOptions(q).map((o) => o.id),
    ["o2"],
  );
  // Retired, never deleted: the question still carries all three.
  assert.equal(q.options.length, 3);
});

test("a twenty-first Option cannot be added, and retired ones do not count against the cap", () => {
  const live = Array.from({ length: MAX_TICKET_QUESTION_OPTIONS }, (_, i) => option(`o${i}`, `Option ${i}`));
  assert.equal(canAddOption(question("q1", "single_choice", live.slice(0, 19))), true);
  assert.equal(canAddOption(question("q1", "single_choice", live)), false);

  // Twenty live plus a retired one is still at the cap, not over it; retire one
  // and there is room again.
  const withRetired = [...live.slice(0, 19), option("gone", "Retired", true)];
  assert.equal(canAddOption(question("q1", "single_choice", withRetired)), true);
});

test("a question that is not a choice one never offers to add an Option", () => {
  assert.equal(canAddOption(question("q1", "short_text")), false);
  assert.equal(canAddOption(question("q1", "checkbox")), false);
});

test("the last live Option cannot be retired", () => {
  assert.equal(canRetireOption(question("q1", "single_choice", [option("o1", "One size")])), false);
  assert.equal(
    canRetireOption(question("q1", "single_choice", [option("o1", "S"), option("o2", "L")])),
    true,
  );
  // A question already down to one live Option and one retired one is still at
  // its floor.
  assert.equal(
    canRetireOption(question("q1", "single_choice", [option("o1", "S"), option("o2", "L", true)])),
    false,
  );
});

test("retired questions are separated from live ones rather than hidden", () => {
  const questions = [
    question("q1", "short_text"),
    question("q2", "short_text", [], true),
    question("q3", "number"),
  ];
  assert.deepEqual(
    liveQuestions(questions).map((q) => q.id),
    ["q1", "q3"],
  );
  assert.deepEqual(
    retiredQuestions(questions).map((q) => q.id),
    ["q2"],
  );
});

test("a move states the whole running order of the live questions", () => {
  const questions = [
    question("q1", "short_text"),
    question("q2", "short_text"),
    question("q3", "short_text"),
  ];
  assert.deepEqual(moveQuestion(questions, "q3", -1), ["q1", "q3", "q2"]);
  assert.deepEqual(moveQuestion(questions, "q1", 1), ["q2", "q1", "q3"]);
});

test("a move off either end leaves the order alone", () => {
  const questions = [question("q1", "short_text"), question("q2", "short_text")];
  assert.deepEqual(moveQuestion(questions, "q1", -1), ["q1", "q2"]);
  assert.deepEqual(moveQuestion(questions, "q2", 1), ["q1", "q2"]);
  assert.deepEqual(moveQuestion(questions, "nobody", 1), ["q1", "q2"]);
});

test("a retired question is not in the order a move states", () => {
  const questions = [
    question("q1", "short_text"),
    question("gone", "short_text", [], true),
    question("q2", "short_text"),
  ];
  assert.deepEqual(moveQuestion(questions, "q2", -1), ["q2", "q1"]);
});

test("a label must be present once trimmed and within its cap", () => {
  assert.equal(isValidLabel("T-shirt size", MAX_TICKET_QUESTION_LABEL_LENGTH), true);
  assert.equal(isValidLabel("   ", MAX_TICKET_QUESTION_LABEL_LENGTH), false);
  assert.equal(isValidLabel("", MAX_TICKET_QUESTION_LABEL_LENGTH), false);
  assert.equal(isValidLabel("a".repeat(MAX_TICKET_QUESTION_LABEL_LENGTH), MAX_TICKET_QUESTION_LABEL_LENGTH), true);
  assert.equal(
    isValidLabel("a".repeat(MAX_TICKET_QUESTION_LABEL_LENGTH + 1), MAX_TICKET_QUESTION_LABEL_LENGTH),
    false,
  );
});

test("the label cap counts characters, so an accent or an emoji is one of them", () => {
  // The API counts runes. Counting UTF-16 units here would refuse a label the
  // API would have accepted, and an organizer would be stopped by a limit that
  // is not the real one.
  assert.equal(isValidLabel("á".repeat(MAX_TICKET_QUESTION_LABEL_LENGTH), MAX_TICKET_QUESTION_LABEL_LENGTH), true);
  assert.equal(isValidLabel("👕".repeat(MAX_TICKET_QUESTION_LABEL_LENGTH), MAX_TICKET_QUESTION_LABEL_LENGTH), true);
});

test("a question is asked only while approved and not retired", () => {
  assert.equal(isAsked(question("q1", "short_text")), true);
  assert.equal(isAsked({ ...question("q1", "short_text"), review_status: "draft" }), false);
  assert.equal(isAsked({ ...question("q1", "short_text"), review_status: "under_review" }), false);
  assert.equal(isAsked({ ...question("q1", "short_text"), review_status: "refused" }), false);
  assert.equal(isAsked(question("q1", "short_text", [], true)), false);
  assert.equal(isAsked({ ...option("o1", "S"), review_status: "draft" }), false);
});

test("the reason shown is the Revocation's, else a refusal's, else nothing", () => {
  assert.deepEqual(
    reviewReason({ review_status: "approved", revocation_reason: "Asks for health data" }),
    { kind: "revocation", reason: "Asks for health data" },
  );
  assert.deepEqual(reviewReason({ review_status: "refused", refusal_reason: "Say why" }), {
    kind: "refusal",
    reason: "Say why",
  });
  // Edited back into a draft: the old refusal no longer describes the row.
  assert.equal(reviewReason({ review_status: "draft", refusal_reason: "Say why" }), null);
  assert.equal(reviewReason({ review_status: "approved", approved_by: "ops@example.com" }), null);
});
