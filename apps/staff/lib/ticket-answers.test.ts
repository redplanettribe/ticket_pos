import assert from "node:assert/strict";
import test from "node:test";

import {
  ANSWERABLE_REFUSAL_KEYS,
  ANSWER_PROBLEM_KEYS,
  answerBodyFor,
  answerProblem,
  chosenOptionIds,
  hasAnswer,
  initialFieldValue,
  selectableOptions,
  toggleOption,
  type Answer,
  type TicketQuestionAnswer,
} from "./ticket-answers.ts";
import type { TicketQuestion, TicketQuestionKind, TicketQuestionOption } from "./ticket-questions.ts";

function option(id: string, label: string, retired = false): TicketQuestionOption {
  return { id, label, sort_order: 0, retired, review_status: "approved" };
}

function question(kind: TicketQuestionKind, options: TicketQuestionOption[] = []): TicketQuestion {
  return {
    id: "q1",
    label: "A question",
    kind,
    required: false,
    timing: "at_checkout",
    sort_order: 0,
    review_status: "approved",
    retired: false,
    options,
  };
}

function answer(partial: Partial<Answer> = {}): Answer {
  return {
    text: null,
    number: null,
    date: null,
    checked: null,
    options: [],
    updated_at: "2026-08-21T12:00:00Z",
    ...partial,
  };
}

function pair(q: TicketQuestion, a: Answer | null = null): TicketQuestionAnswer {
  return { question: q, answer: a };
}

test("a retired Option stays selectable when this Answer already chose it", () => {
  const q = question("multi_choice", [
    option("veg", "Vegetarian", true),
    option("vegan", "Vegan", true),
    option("nuts", "Nuts"),
  ]);
  const chosen = pair(
    q,
    answer({
      options: [
        { option_id: "veg", label: "Vegetarian", current_label: "Vegetarian", retired: true },
      ],
    }),
  );

  // The retired Option this Answer holds is still on the form — posting the
  // Answer back without it would silently drop what somebody said. The other
  // retired one has left the list and is not offered.
  assert.deepEqual(
    selectableOptions(chosen).map((o) => o.id),
    ["veg", "nuts"],
  );
  // Nothing chosen: only the live Option is offered.
  assert.deepEqual(
    selectableOptions(pair(q)).map((o) => o.id),
    ["nuts"],
  );
});

test("chosenOptionIds reads the Answer's selection in order", () => {
  const q = question("multi_choice", [option("a", "A"), option("b", "B")]);
  const held = pair(
    q,
    answer({
      options: [
        { option_id: "b", label: "B", current_label: "B", retired: false },
        { option_id: "a", label: "A", current_label: "A", retired: false },
      ],
    }),
  );
  assert.deepEqual(chosenOptionIds(held), ["b", "a"]);
  assert.deepEqual(chosenOptionIds(pair(q)), []);
});

test("an unticked checkbox is an Answer, and no Answer is not", () => {
  const q = question("checkbox");
  // Somebody who read the question and said no has said something. Clearing it
  // back to "not said" is a different act from unticking it.
  assert.equal(hasAnswer(pair(q, answer({ checked: false }))), true);
  assert.equal(hasAnswer(pair(q)), false);
});

test("initialFieldValue starts a field at whatever is stored", () => {
  assert.equal(initialFieldValue(pair(question("short_text"), answer({ text: "M" }))), "M");
  // A number arrives as digits and stays digits: nothing parses it, so nothing
  // rounds it.
  assert.equal(initialFieldValue(pair(question("number"), answer({ number: "4.50" }))), "4.50");
  assert.equal(
    initialFieldValue(pair(question("date"), answer({ date: "2026-09-01" }))),
    "2026-09-01",
  );
  assert.equal(initialFieldValue(pair(question("short_text"))), "");
});

test("answerBodyFor sends the one field its question's kind takes", () => {
  assert.deepEqual(answerBodyFor(question("short_text"), "M", false, []), { text: "M" });
  assert.deepEqual(answerBodyFor(question("long_text"), "Coeliac", false, []), { text: "Coeliac" });
  assert.deepEqual(answerBodyFor(question("number"), "3", false, []), { number: "3" });
  assert.deepEqual(answerBodyFor(question("date"), "2026-09-01", false, []), {
    date: "2026-09-01",
  });
  assert.deepEqual(answerBodyFor(question("single_choice"), "", false, ["m"]), {
    option_ids: ["m"],
  });
});

test("an unticked checkbox is always sendable, and an empty field is not", () => {
  // false IS an answer, so a checkbox always has a body.
  assert.deepEqual(answerBodyFor(question("checkbox"), "", false, []), { checked: false });
  // An empty field is not an Answer of "". The way to say "not said" is to clear
  // the Answer, which is a DELETE and not this.
  assert.equal(answerBodyFor(question("short_text"), "   ", false, []), null);
  assert.equal(answerBodyFor(question("number"), "", false, []), null);
  assert.equal(answerBodyFor(question("date"), "", false, []), null);
  assert.equal(answerBodyFor(question("single_choice"), "", false, []), null);
});

test("single_choice replaces and multi_choice accumulates", () => {
  const single = question("single_choice", [option("s", "S"), option("m", "M")]);
  assert.deepEqual(toggleOption(single, ["s"], "m"), ["m"]);
  // Picking the one already picked clears it — the form has no other way back to
  // "nothing selected", and the API refuses an empty selection as an Answer.
  assert.deepEqual(toggleOption(single, ["m"], "m"), []);

  const multi = question("multi_choice", [option("a", "A"), option("b", "B")]);
  assert.deepEqual(toggleOption(multi, ["a"], "b"), ["a", "b"]);
  assert.deepEqual(toggleOption(multi, ["a", "b"], "a"), ["b"]);
});

test("answerProblem reads the token the API refused with, and nothing else", () => {
  assert.equal(answerProblem({ kind: "number", problem: "not_a_number" }), "not_a_number");
  // A token this app has not heard of falls through to the API's own sentence,
  // which is the floor ADR 0023 puts under every unknown code.
  assert.equal(answerProblem({ problem: "invented" }), null);
  assert.equal(answerProblem(null), null);
  assert.equal(answerProblem("not an object"), null);
});

test("every refusal and problem token has a catalog key", () => {
  // The maps are what keep a token from being turned into a word at a call site,
  // which is how there come to be two Spanish sentences for one refusal.
  assert.deepEqual(Object.keys(ANSWERABLE_REFUSAL_KEYS).sort(), [
    "event_started",
    "sale_reversed",
  ]);
  for (const key of Object.values(ANSWER_PROBLEM_KEYS)) {
    assert.equal(typeof key, "string");
    assert.notEqual(key, "");
  }
});
