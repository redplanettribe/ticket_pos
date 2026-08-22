import assert from "node:assert/strict";
import { test } from "node:test";

import type { Question, QuestionAnswer, QuestionKind } from "./ticket-questions.ts";
import {
  closedWindowKey,
  collapsesAfterSave,
  panelDisclosure,
  saveTrigger,
} from "./held-ticket-panel.ts";

/**
 * The held-Ticket panel's rules (#345, ADR 0049): when it is open, when it
 * folds, and what press saves an Answer. One set of rules for the buyer's
 * "Your ticket" and for a Holder's Customer Area, which are the same panel.
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

function unanswered(overrides: Partial<Question> = {}): QuestionAnswer {
  return { question: question(overrides), answer: null };
}

// STORY 13: a Ticket that asks nothing has no expand control at all.
test("a ticket with no questions has no panel to expand", () => {
  assert.equal(panelDisclosure({ outstanding_count: 0, questions: [] }), "none");
});

// A retired question this Ticket never answered is not drawn, so it does not
// earn the panel an expand control either.
test("a ticket whose only question is retired and unanswered has nothing to expand", () => {
  assert.equal(
    panelDisclosure({ outstanding_count: 0, questions: [unanswered({ retired: true })] }),
    "none",
  );
});

// STORY 7: open while an Answer is owed.
test("the panel is open while an answer is outstanding", () => {
  assert.equal(panelDisclosure({ outstanding_count: 1, questions: [unanswered()] }), "open");
});

// STORY 11 and 12: collapsed once nothing is owed — and an unrequired,
// unanswered question is not owed, because the API's count does not include
// it. The count is the API's; this side never recomputes it.
test("the panel is collapsed when nothing is owed, an optional unanswered question included", () => {
  assert.equal(
    panelDisclosure({ outstanding_count: 0, questions: [unanswered({ required: false })] }),
    "collapsed",
  );
});

// STORY 10: only the LAST owed Answer folds the panel.
test("the panel folds when the outstanding count reaches zero from above", () => {
  assert.equal(collapsesAfterSave(1, 0), true);
  assert.equal(collapsesAfterSave(3, 0), true);
});

test("a save that still leaves something owed keeps the panel open", () => {
  assert.equal(collapsesAfterSave(2, 1), false);
});

test("a save on a reopened panel that owed nothing does not snap it shut", () => {
  assert.equal(collapsesAfterSave(0, 0), false);
});

// STORIES 8 and 9: what press persists an Answer of each kind.
test("choice, tick box and date save on change; text and number save on blur", () => {
  const expected: Record<QuestionKind, "change" | "blur"> = {
    short_text: "blur",
    long_text: "blur",
    number: "blur",
    date: "change",
    checkbox: "change",
    single_choice: "change",
    multi_choice: "change",
  };
  for (const [kind, trigger] of Object.entries(expected)) {
    assert.equal(saveTrigger(kind as QuestionKind), trigger, kind);
  }
});

// STORY 21: the closed window is said in the reader's language, from the
// API's token, and an unknown token is still a shut window.
test("the closed window is keyed from the API's refusal token", () => {
  assert.equal(closedWindowKey({ answerable: true, answerable_refusal: "" }), null);
  assert.equal(
    closedWindowKey({ answerable: false, answerable_refusal: "event_started" }),
    "closedStarted",
  );
  assert.equal(
    closedWindowKey({ answerable: false, answerable_refusal: "sale_reversed" }),
    "closedReversed",
  );
  assert.equal(
    closedWindowKey({ answerable: false, answerable_refusal: "something_new" }),
    "closedUnknown",
  );
});
