import assert from "node:assert/strict";
import test from "node:test";

import type { BuyerTicket } from "./buyer-answers.ts";
import { answerKey, type AnswerValues } from "./checkout-answers.ts";
import {
  reassignmentAnswerBodies,
  reassignmentOwed,
  reassignmentSlot,
  refusedQuestionIds,
} from "./reassignment.ts";

const size = {
  id: "q-size",
  label: "T-shirt size",
  kind: "single_choice" as const,
  required: true,
  options: [
    { id: "opt-s", label: "S" },
    { id: "opt-m", label: "M" },
  ],
};

const note = {
  id: "q-note",
  label: "Anything else?",
  kind: "long_text" as const,
  required: false,
  options: [],
};

const waiver = {
  id: "q-waiver",
  label: "I accept the waiver",
  kind: "checkbox" as const,
  required: true,
  options: [],
};

function ticket(overrides: Partial<BuyerTicket> = {}): BuyerTicket {
  return {
    ticket_id: "tk-2",
    ordinal: 2,
    ticket_type_name: "GA",
    assignment_state: "assigned",
    holder_email: "carla@example.com",
    assignable: true,
    reassignment_questions: [size, note],
    ...overrides,
  };
}

function valuesFor(slot: NonNullable<ReturnType<typeof reassignmentSlot>>, values: Record<string, object>): AnswerValues {
  const keyed: AnswerValues = {};
  for (const [questionId, value] of Object.entries(values)) {
    keyed[answerKey(slot.ticketTypeId, slot.index, questionId)] = value;
  }
  return keyed;
}

test("reassignmentSlot is the Ticket's questions when the API sent them, and nothing otherwise", () => {
  const slot = reassignmentSlot(ticket());
  assert.ok(slot);
  assert.equal(slot.index, 2);
  assert.deepEqual(
    slot.questions.map((question) => question.id),
    ["q-size", "q-note"],
  );

  // Off a Named Tickets Event, after the doors, or on a Ticket Type that
  // asks nothing, the API sends no field: the form is the address alone.
  assert.equal(reassignmentSlot(ticket({ reassignment_questions: undefined })), null);
  assert.equal(reassignmentSlot(ticket({ reassignment_questions: [] })), null);
});

test("reassignmentSlot keeps two Tickets of one Sale apart", () => {
  const first = reassignmentSlot(ticket({ ticket_id: "tk-1", ordinal: 1 }));
  const second = reassignmentSlot(ticket());
  assert.ok(first && second);
  assert.notEqual(
    answerKey(first.ticketTypeId, first.index, "q-size"),
    answerKey(second.ticketTypeId, second.index, "q-size"),
  );
});

test("reassignmentOwed is the required questions without a usable Answer, in the order asked", () => {
  const slot = reassignmentSlot(ticket({ reassignment_questions: [size, note, waiver] }));
  assert.ok(slot);
  // The waiver is a required checkbox drawn unticked, so it says "no" and is
  // answered; the size is owed; the note is optional and never owed.
  assert.deepEqual(reassignmentOwed(slot, {}), ["q-size"]);
  assert.deepEqual(reassignmentOwed(slot, valuesFor(slot, { "q-size": { option_ids: ["opt-m"] } })), []);
  // An Option the question does not offer is not an Answer the server keeps.
  assert.deepEqual(reassignmentOwed(slot, valuesFor(slot, { "q-size": { option_ids: ["opt-xl"] } })), ["q-size"]);
});

test("reassignmentAnswerBodies sends what was said, the drawn required checkbox included", () => {
  const slot = reassignmentSlot(ticket({ reassignment_questions: [size, note, waiver] }));
  assert.ok(slot);
  const bodies = reassignmentAnswerBodies(
    slot,
    valuesFor(slot, { "q-size": { option_ids: ["opt-s"] }, "q-note": { text: "   " } }),
  );
  assert.deepEqual(bodies, [
    { ticket_question_id: "q-size", option_ids: ["opt-s"] },
    // A blank note is not said, and does not travel.
    { ticket_question_id: "q-waiver", checked: false },
  ]);
});

test("refusedQuestionIds reads the questions a NAMED_TICKETS_INCOMPLETE refusal names", () => {
  assert.deepEqual(
    refusedQuestionIds({
      tickets: [
        { ticket_type_id: "tt-ga", ticket_index: 2, holder_email_missing: false, missing_question_ids: ["q-size"] },
      ],
    }),
    ["q-size"],
  );
  assert.deepEqual(refusedQuestionIds(undefined), []);
  assert.deepEqual(refusedQuestionIds({ tickets: "nope" }), []);
});
