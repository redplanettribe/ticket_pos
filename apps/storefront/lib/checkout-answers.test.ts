import assert from "node:assert/strict";
import test from "node:test";

import {
  answerKey,
  answerSlots,
  checkoutAnswerBodies,
  hasCheckoutQuestions,
  ownTicketSlot,
  type AnsweredTicketType,
  type AnswerValues,
} from "./checkout-answers.ts";

const size = {
  id: "q-size",
  label: "T-shirt size",
  kind: "short_text" as const,
  required: true,
  options: [],
};

const meal = {
  id: "q-meal",
  label: "Meal",
  kind: "single_choice" as const,
  required: false,
  options: [
    { id: "opt-chicken", label: "Chicken" },
    { id: "opt-veg", label: "Vegetarian" },
  ],
};

// The catalog, cheap-to-dear as Organizations in fact list them — so "first in
// the catalog" and "the dearest" are never the same line here.
const generalAdmission: AnsweredTicketType = {
  id: "tt-ga",
  name: "General Admission",
  price_cents: 2000,
  ticket_questions: [size, meal],
};

const premium: AnsweredTicketType = {
  id: "tt-premium",
  name: "Premium",
  price_cents: 5000,
  ticket_questions: [size],
};

const vip: AnsweredTicketType = {
  id: "tt-vip",
  name: "VIP",
  price_cents: 9000,
  ticket_questions: [],
};

// A cart of three of one Ticket Type presents THREE separate answer sets. This
// is the acceptance criterion the whole section exists for: the buyer of four
// tickets is not one person answering once.
test("answerSlots gives one set of questions per ticket in the cart", () => {
  const slots = answerSlots([generalAdmission], { "tt-ga": 3 });

  assert.equal(slots.length, 3);
  assert.deepEqual(
    slots.map((slot) => slot.index),
    [1, 2, 3],
  );
  for (const slot of slots) {
    assert.equal(slot.ticketTypeId, "tt-ga");
    assert.equal(slot.ticketTypeName, "General Admission");
    assert.deepEqual(
      slot.questions.map((question) => question.id),
      ["q-size", "q-meal"],
    );
  }
});

// The indices are ONE-BASED and count against the Ticket Type across the whole
// cart, because that is what the API's ticket_index means: it becomes the minted
// Ticket's ordinal, which starts at 1.
test("answerSlots numbers tickets from one", () => {
  const slots = answerSlots([generalAdmission], { "tt-ga": 1 });
  assert.deepEqual(
    slots.map((slot) => slot.index),
    [1],
  );
});

// A Ticket Type that asks nothing contributes no section, and neither does one
// nobody is buying. A cart of only such types has no answer section at all.
test("answerSlots skips ticket types with no questions and none in the cart", () => {
  assert.deepEqual(answerSlots([vip], { "tt-vip": 2 }), []);
  assert.deepEqual(answerSlots([generalAdmission], { "tt-ga": 0 }), []);
  assert.deepEqual(answerSlots([generalAdmission, vip], {}), []);
});

// The order is the cart's order of Ticket Types and then the ticket's own
// number, so the form reads the way the cart does.
test("answerSlots keeps ticket types in the order the page lists them", () => {
  const both: AnsweredTicketType = { ...vip, ticket_questions: [size] };
  const slots = answerSlots([generalAdmission, both], { "tt-ga": 1, "tt-vip": 2 });
  assert.deepEqual(
    slots.map((slot) => `${slot.ticketTypeId}#${slot.index}`),
    ["tt-ga#1", "tt-vip#1", "tt-vip#2"],
  );
});

test("hasCheckoutQuestions reports whether anything in the cart asks something", () => {
  assert.equal(hasCheckoutQuestions([generalAdmission], { "tt-ga": 1 }), true);
  assert.equal(hasCheckoutQuestions([vip], { "tt-vip": 1 }), false);
  assert.equal(hasCheckoutQuestions([], {}), false);
});

test("answerKey identifies one ticket's reply to one question", () => {
  assert.equal(answerKey("tt-ga", 2, "q-size"), "tt-ga:2:q-size");
  // Distinct in every dimension: two tickets of one type, and two questions on
  // one ticket, must never collide into one field.
  assert.notEqual(answerKey("tt-ga", 1, "q-size"), answerKey("tt-ga", 2, "q-size"));
  assert.notEqual(answerKey("tt-ga", 1, "q-size"), answerKey("tt-ga", 1, "q-meal"));
});

test("checkoutAnswerBodies sends what the buyer filled in, in the slot the kind takes", () => {
  const slots = answerSlots([generalAdmission], { "tt-ga": 2 });
  const values: AnswerValues = {
    "tt-ga:1:q-size": { text: "M" },
    "tt-ga:2:q-meal": { option_ids: ["opt-veg"] },
  };

  assert.deepEqual(checkoutAnswerBodies(slots, values), [
    { ticket_type_id: "tt-ga", ticket_index: 1, ticket_question_id: "q-size", text: "M" },
    {
      ticket_type_id: "tt-ga",
      ticket_index: 2,
      ticket_question_id: "q-meal",
      option_ids: ["opt-veg"],
    },
  ]);
});

// SKIPPING IS THE ORDINARY CASE. A section nobody touched sends nothing, and the
// checkout is exactly the one that existed before this feature.
test("checkoutAnswerBodies sends nothing for a section nobody touched", () => {
  const slots = answerSlots([generalAdmission], { "tt-ga": 3 });
  assert.deepEqual(checkoutAnswerBodies(slots, {}), []);
});

// A field the buyer opened and left blank is not an answer. Sending it would put
// an empty reply where "not said" belongs, and the API would drop it anyway —
// dropping it here saves the round trip carrying it.
test("checkoutAnswerBodies drops blank and whitespace-only replies", () => {
  const slots = answerSlots([generalAdmission], { "tt-ga": 1 });
  assert.deepEqual(
    checkoutAnswerBodies(slots, {
      "tt-ga:1:q-size": { text: "   " },
      "tt-ga:1:q-meal": { option_ids: [] },
    }),
    [],
  );
});

// FALSE IS AN ANSWER. Somebody who read "I will attend the dinner" and left it
// unticked has said no, which is a different fact from never having been asked —
// so an untouched checkbox sends nothing and a deliberately-false one sends
// false.
test("checkoutAnswerBodies distinguishes an unticked checkbox from an untouched one", () => {
  const dinner = {
    id: "q-dinner",
    label: "Dinner",
    kind: "checkbox" as const,
    required: false,
    options: [],
  };
  const slots = answerSlots(
    [{ id: "tt-ga", name: "GA", price_cents: 2000, ticket_questions: [dinner] }],
    { "tt-ga": 2 },
  );

  assert.deepEqual(checkoutAnswerBodies(slots, { "tt-ga:1:q-dinner": { checked: false } }), [
    { ticket_type_id: "tt-ga", ticket_index: 1, ticket_question_id: "q-dinner", checked: false },
  ]);
});

// A value left behind by a ticket the buyer then removed from the cart must not
// travel: the slots are the truth about what is being bought, and the values are
// only what was typed into them.
test("checkoutAnswerBodies ignores values with no slot left in the cart", () => {
  const slots = answerSlots([generalAdmission], { "tt-ga": 1 });
  assert.deepEqual(
    checkoutAnswerBodies(slots, {
      "tt-ga:1:q-size": { text: "M" },
      "tt-ga:3:q-size": { text: "L" },
      "tt-vip:1:q-size": { text: "S" },
    }),
    [{ ticket_type_id: "tt-ga", ticket_index: 1, ticket_question_id: "q-size", text: "M" }],
  );
});

// The number reply travels as the digits were TYPED. It lands in a NUMERIC
// column, and a JSON number would round-trip through a float — which is how
// `3.50` loses the zero a price or a measurement meant.
test("checkoutAnswerBodies sends a number as a string", () => {
  const age = {
    id: "q-age",
    label: "Age",
    kind: "number" as const,
    required: false,
    options: [],
  };
  const slots = answerSlots(
    [{ id: "tt-ga", name: "GA", price_cents: 2000, ticket_questions: [age] }],
    { "tt-ga": 1 },
  );
  const [body] = checkoutAnswerBodies(slots, { "tt-ga:1:q-age": { number: "3.50" } });
  assert.equal(body?.number, "3.50");
});

// CHECKOUT ASKS ABOUT THE BUYER'S OWN TICKET AND NO OTHER (ADR 0048): the first
// ticket of the DEAREST line the cart holds (ADR 0074), whatever the catalog's
// order and whatever the quantities — the other two of three are their Holders'
// to answer. The same choice the commit spine makes, so the questions on the
// page belong to the Ticket the buyer will in fact hold.
test("ownTicketSlot is the first ticket of the dearest type in the cart", () => {
  const slot = ownTicketSlot([generalAdmission, premium], { "tt-ga": 3, "tt-premium": 1 }, true);
  assert.deepEqual(slot, {
    ticketTypeId: "tt-premium",
    ticketTypeName: "Premium",
    index: 1,
    questions: [size],
  });
});

// A type nobody is buying never takes the seat, however dear it is.
test("ownTicketSlot ignores types the cart does not hold", () => {
  const slot = ownTicketSlot([generalAdmission, premium], { "tt-ga": 2, "tt-premium": 0 }, true);
  assert.deepEqual(slot, {
    ticketTypeId: "tt-ga",
    ticketTypeName: "General Admission",
    index: 1,
    questions: [size, meal],
  });
});

// EQUALLY PRICED LINES TIE-BREAK ON THE CATALOG'S ORDER, exactly as the whole
// rule used to: the list arrives in that order, so the first of the equals wins.
test("ownTicketSlot tie-breaks equally priced types on the catalog's order", () => {
  const balcony: AnsweredTicketType = {
    id: "tt-balcony",
    name: "Balcony",
    price_cents: generalAdmission.price_cents,
    ticket_questions: [meal],
  };
  const slot = ownTicketSlot([generalAdmission, balcony], { "tt-ga": 1, "tt-balcony": 1 }, true);
  assert.equal(slot?.ticketTypeId, "tt-ga");
});

// THE PRICE IS THE ONE THE BUYER PAYS. `price_cents` is the effective buyer
// price the API computes — already the Promotional Price where one is live — so
// a discounted dear line loses to a cheaper line sold at its List Price, which
// is what the commit spine compares too.
test("ownTicketSlot compares the price as sold, not the list price", () => {
  // Premium's List Price is 5000; a live Promotion has the API reporting 500,
  // which is what this app is given and all it ever compares.
  const discountedPremium: AnsweredTicketType = { ...premium, price_cents: 500 };
  const slot = ownTicketSlot(
    [generalAdmission, discountedPremium],
    { "tt-ga": 1, "tt-premium": 1 },
    true,
  );
  assert.equal(slot?.ticketTypeId, "tt-ga");
});

// The dearest line wins even when it asks nothing: a buyer holding a VIP that
// asks no questions holds the VIP, and is asked nothing.
test("ownTicketSlot is null when the buyer's own ticket type asks nothing", () => {
  assert.equal(ownTicketSlot([generalAdmission, vip], { "tt-ga": 2, "tt-vip": 1 }, true), null);
});

test("ownTicketSlot is null with assignment closed or an empty cart", () => {
  assert.equal(ownTicketSlot([generalAdmission], { "tt-ga": 3 }, false), null);
  assert.equal(ownTicketSlot([generalAdmission], { "tt-ga": 0 }, true), null);
});
