import assert from "node:assert/strict";
import test from "node:test";

import {
  answerBodyFor,
  chosenOptionIds,
  initialChecked,
  initialFieldValue,
  isReadOnly,
  selectableOptions,
  toggleOption,
  visibleQuestions,
  type Question,
  type QuestionAnswer,
  type QuestionKind,
  type QuestionOption,
} from "./ticket-questions.ts";

function option(id: string, label: string, retired = false): QuestionOption {
  return { id, label, sort_order: 0, retired };
}

function question(kind: QuestionKind, options: QuestionOption[] = [], retired = false): Question {
  return {
    id: "q1",
    label: "T-shirt size",
    kind,
    required: true,
    sort_order: 0,
    retired,
    options,
  };
}

function pair(q: Question, answer: QuestionAnswer["answer"] = null): QuestionAnswer {
  return { question: q, answer };
}

function answered(fields: Partial<NonNullable<QuestionAnswer["answer"]>>) {
  return {
    text: null,
    number: null,
    date: null,
    checked: null,
    options: [],
    updated_at: "2026-08-01T00:00:00Z",
    ...fields,
  };
}

// The body a form posts is decided by the QUESTION's kind and never guessed from
// what the field happens to hold. The API refuses a body of the wrong shape
// rather than coercing it, so a form that guessed would be refused, and rightly.
test("each kind produces only its own field", () => {
  assert.deepEqual(answerBodyFor(question("short_text"), "M", false, []), { text: "M" });
  assert.deepEqual(answerBodyFor(question("long_text"), "No nuts", false, []), { text: "No nuts" });
  assert.deepEqual(answerBodyFor(question("number"), "3.50", false, []), { number: "3.50" });
  assert.deepEqual(answerBodyFor(question("date"), "2026-09-01", false, []), { date: "2026-09-01" });
  assert.deepEqual(answerBodyFor(question("single_choice"), "", false, ["o1"]), {
    option_ids: ["o1"],
  });
  assert.deepEqual(answerBodyFor(question("multi_choice"), "", false, ["o1", "o2"]), {
    option_ids: ["o1", "o2"],
  });
});

// A number question's digits go over the wire AS TYPED. `3.50` means its
// trailing zero when the question asks a price or a measurement, and nothing on
// this side is allowed to normalize it away.
test("a number keeps the digits as typed", () => {
  assert.deepEqual(answerBodyFor(question("number"), "3.50", false, []), { number: "3.50" });
  assert.deepEqual(answerBodyFor(question("number"), "-0.000001", false, []), {
    number: "-0.000001",
  });
});

// FALSE IS AN ANSWER. Somebody who read "I will attend the dinner" and left it
// unticked has said no, which is a different fact from never having been asked —
// so a checkbox is always sendable and never falls through to "nothing to send".
test("an unticked checkbox is still an answer", () => {
  assert.deepEqual(answerBodyFor(question("checkbox"), "", false, []), { checked: false });
  assert.deepEqual(answerBodyFor(question("checkbox"), "", true, []), { checked: true });
});

// An empty field is not an Answer of "". This page offers no way to take an
// Answer back — whoever holds the link may OVERWRITE what is there, and erasing
// somebody else's reply from an unauthenticated page is not a power it grants.
test("an empty field sends nothing at all", () => {
  for (const kind of ["short_text", "long_text", "number", "date"] as const) {
    assert.equal(answerBodyFor(question(kind), "   ", false, []), null, kind);
  }
  assert.equal(answerBodyFor(question("single_choice"), "", false, []), null);
  assert.equal(answerBodyFor(question("multi_choice"), "", false, []), null);
});

// A RETIRED OPTION THIS ANSWER ALREADY CHOSE STAYS ON THE FORM. Dropping it
// would silently discard what somebody said the next time the Answer was posted
// back — "kept on the Tickets that chose it" has to survive a correction and not
// only a read. A retired Option nobody chose is gone, because it is not on offer.
test("a retired option stays only where it was already chosen", () => {
  const live = option("o1", "Small");
  const gone = option("o2", "Medium");
  gone.retired = true;
  const q = question("multi_choice", [live, gone]);

  assert.deepEqual(
    selectableOptions(pair(q)).map((o) => o.id),
    ["o1"],
  );
  assert.deepEqual(
    selectableOptions(
      pair(
        q,
        answered({
          options: [{ option_id: "o2", label: "Medium", current_label: "Medium", retired: true }],
        }),
      ),
    ).map((o) => o.id),
    ["o1", "o2"],
  );
});

// single_choice REPLACES and multi_choice ACCUMULATES. A shared toggle would let
// a single_choice form build a body the API must refuse.
test("toggling respects the kind", () => {
  const single = question("single_choice", [option("o1", "S"), option("o2", "M")]);
  assert.deepEqual(toggleOption(single, ["o1"], "o2"), ["o2"]);
  assert.deepEqual(toggleOption(single, ["o1"], "o1"), []);

  const multi = question("multi_choice", [option("o1", "S"), option("o2", "M")]);
  assert.deepEqual(toggleOption(multi, ["o1"], "o2"), ["o1", "o2"]);
  assert.deepEqual(toggleOption(multi, ["o1", "o2"], "o1"), ["o2"]);
});

// The form starts on WHAT IS STORED, which may be the buyer's guess from
// checkout. The holder sees it and may overwrite it — somebody cannot correct a
// guess they cannot see.
test("a field starts on whatever was already answered", () => {
  assert.equal(initialFieldValue(pair(question("short_text"))), "");
  assert.equal(initialFieldValue(pair(question("short_text"), answered({ text: "S" }))), "S");
  assert.equal(initialFieldValue(pair(question("number"), answered({ number: "3.50" }))), "3.50");
  assert.equal(
    initialFieldValue(pair(question("date"), answered({ date: "2026-09-01" }))),
    "2026-09-01",
  );

  // An absent checkbox answer is not the same fact as a false one, but both
  // start the box unticked — the difference lives in the API, which stores one
  // and not the other.
  assert.equal(initialChecked(pair(question("checkbox"))), false);
  assert.equal(initialChecked(pair(question("checkbox"), answered({ checked: false }))), false);
  assert.equal(initialChecked(pair(question("checkbox"), answered({ checked: true }))), true);

  assert.deepEqual(
    chosenOptionIds(
      pair(
        question("multi_choice"),
        answered({
          options: [
            { option_id: "o2", label: "M", current_label: "M", retired: false },
            { option_id: "o1", label: "S", current_label: "S", retired: false },
          ],
        }),
      ),
    ),
    // In the order the Answer holds them, which is the order it was given in.
    ["o2", "o1"],
  );
});

// A retired question is one the Organization has stopped asking. Offering it
// afresh would collect an answer nobody wants; dropping one this Ticket HAS
// answered would hide from the holder something recorded about them.
test("a retired question is shown only if it was answered, and never editable", () => {
  const retired = question("short_text", [], true);
  const live = question("short_text");
  live.id = "q2";

  const view = {
    event_name: "Fest",
    ticket_type_name: "GA",
    questions: [pair(retired), pair(live)],
  };
  assert.deepEqual(
    visibleQuestions(view).map((p) => p.question.id),
    ["q2"],
  );

  const withAnswer = {
    ...view,
    questions: [pair(retired, answered({ text: "S" })), pair(live)],
  };
  assert.deepEqual(
    visibleQuestions(withAnswer).map((p) => p.question.id),
    ["q1", "q2"],
  );
  assert.equal(isReadOnly(pair(retired, answered({ text: "S" }))), true);
  assert.equal(isReadOnly(pair(live)), false);
});
