import assert from "node:assert/strict";
import test from "node:test";

import {
  answeredPairs,
  answeredValue,
  lastAnswerReminder,
  ticketAnswersVisible,
  ticketOwes,
} from "./dossier-ticket-answers.ts";
import type { Answer, TicketQuestionAnswer } from "./ticket-answers.ts";
import type { TicketQuestion, TicketQuestionKind } from "./ticket-questions.ts";

const question = (kind: TicketQuestionKind, fields: Partial<TicketQuestion> = {}): TicketQuestion => ({
  id: "q_1",
  label: "Shirt size",
  kind,
  required: true,
  timing: "after_purchase",
  sort_order: 1,
  retired: false,
  options: [],
  review_status: "approved",
  refusal_reason: "",
  revocation_reason: "",
  ...fields,
} as TicketQuestion);

const answer = (fields: Partial<Answer> = {}): Answer => ({
  text: null,
  number: null,
  date: null,
  checked: null,
  options: [],
  updated_at: "2026-09-01T12:00:00Z",
  ...fields,
});

const pair = (kind: TicketQuestionKind, fields: Partial<Answer>): TicketQuestionAnswer => ({
  question: question(kind),
  answer: answer(fields),
});

// --- visibility ------------------------------------------------------------------

// The questions flag closed: the API omits every key, so there is nothing to draw.
test("a Ticket without answers has no Answers section", () => {
  assert.equal(ticketAnswersVisible({}), false);
});

test("a Ticket with answers, even none, has an Answers section", () => {
  assert.equal(ticketAnswersVisible({ answers: [] }), true);
});

// --- answeredPairs -----------------------------------------------------------------

test("answeredPairs keeps the answered pairs in the order the API gave", () => {
  const first = pair("short_text", { text: "M" });
  const second = pair("number", { number: "3" });
  assert.deepEqual(answeredPairs({ answers: [first, second] }), [first, second]);
});

test("answeredPairs drops an unanswered pair should one ever arrive", () => {
  const answered = pair("short_text", { text: "M" });
  assert.deepEqual(answeredPairs({ answers: [{ question: question("date"), answer: null }, answered] }), [answered]);
});

test("answeredPairs is empty with the flag closed", () => {
  assert.deepEqual(answeredPairs({}), []);
});

// --- ticketOwes --------------------------------------------------------------------

const outstanding = { question_id: "q_2", label: "Diet", kind: "short_text" as const, sort_order: 2 };

test("a Ticket owing Answers names them", () => {
  assert.deepEqual(ticketOwes({ outstanding_answers: [outstanding] }), {
    state: "owes",
    questions: [outstanding],
  });
});

test("a Ticket owing none says so", () => {
  assert.deepEqual(ticketOwes({ outstanding_answers: [] }), { state: "nothing" });
});

// A reversed Sale's Tickets owe nothing and are not drawn as owing nothing either.
test("a Ticket with no outstanding_answers has no debt to draw", () => {
  assert.deepEqual(ticketOwes({ outstanding_answers: undefined }), { state: "hidden" });
  assert.deepEqual(ticketOwes({}), { state: "hidden" });
});

// --- lastAnswerReminder -------------------------------------------------------------

test("the last Answer Reminder is drawn when one was sent", () => {
  assert.deepEqual(lastAnswerReminder({ last_answer_reminder_sent_at: "2026-09-02T10:00:00Z" }), {
    state: "sent",
    at: "2026-09-02T10:00:00Z",
  });
});

test("a Ticket never reminded says so", () => {
  assert.deepEqual(lastAnswerReminder({ last_answer_reminder_sent_at: null }), { state: "never" });
});

test("a Ticket with no reminder key has no reminder line", () => {
  assert.deepEqual(lastAnswerReminder({}), { state: "hidden" });
});

// --- answeredValue ------------------------------------------------------------------

test("a text Answer is its own words, short or long", () => {
  assert.deepEqual(answeredValue(pair("short_text", { text: "M" })), { kind: "text", text: "M" });
  assert.deepEqual(answeredValue(pair("long_text", { text: "No nuts\nplease" })), {
    kind: "text",
    text: "No nuts\nplease",
  });
});

test("a number Answer keeps its digits exactly", () => {
  assert.deepEqual(answeredValue(pair("number", { number: "12345678901234567890.5" })), {
    kind: "number",
    digits: "12345678901234567890.5",
  });
});

test("a date Answer is the calendar day it names", () => {
  assert.deepEqual(answeredValue(pair("date", { date: "2026-03-01" })), { kind: "date", day: "2026-03-01" });
});

// false is an Answer: somebody left it unticked.
test("a checkbox Answer is yes or no, and false is no", () => {
  assert.deepEqual(answeredValue(pair("checkbox", { checked: true })), { kind: "checkbox", checked: true });
  assert.deepEqual(answeredValue(pair("checkbox", { checked: false })), { kind: "checkbox", checked: false });
});

test("a choice Answer reads the labels as they were when chosen", () => {
  const options = [
    { option_id: "o_1", label: "Vegan", current_label: "Plant-based", retired: false },
    { option_id: "o_2", label: "Gluten free", current_label: "Gluten free", retired: true },
  ];
  assert.deepEqual(answeredValue(pair("multi_choice", { options })), {
    kind: "choice",
    labels: ["Vegan", "Gluten free"],
  });
  assert.deepEqual(answeredValue(pair("single_choice", { options: options.slice(0, 1) })), {
    kind: "choice",
    labels: ["Vegan"],
  });
});

test("an Answer whose shape does not fit its kind is unreadable rather than guessed at", () => {
  assert.deepEqual(answeredValue(pair("number", { text: "3" })), { kind: "unreadable" });
  assert.deepEqual(answeredValue(pair("checkbox", {})), { kind: "unreadable" });
  assert.deepEqual(answeredValue(pair("single_choice", {})), { kind: "unreadable" });
  assert.deepEqual(answeredValue({ question: question("date"), answer: null }), { kind: "unreadable" });
});
