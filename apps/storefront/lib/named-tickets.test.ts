import assert from "node:assert/strict";
import test from "node:test";

import {
  answerKey,
  selfHeldSeat,
  type AnsweredTicketType,
  type AnswerValues,
} from "./checkout-answers.ts";
import {
  answerIsUsable,
  checkoutHolderBodies,
  holderKey,
  holderKeyOfField,
  namedTicketsApply,
  namedTicketSlots,
  namedTicketsOwed,
  owedQuestionIds,
  parseCheckoutHolders,
  questionErrorCopy,
  questionErrorKinds,
  refusedTickets,
  surrenderedTicketTypeId,
  withDrawnCheckboxes,
  type HolderValues,
  type NamedTicketsForm,
} from "./named-tickets.ts";

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

const meal = {
  id: "q-meal",
  label: "Meal",
  kind: "short_text" as const,
  required: false,
  options: [],
};

const diet = {
  id: "q-diet",
  label: "Dietary needs",
  kind: "long_text" as const,
  required: true,
  options: [],
};

// The catalog cheap-to-dear, as Organizations list it, so "first in the
// catalog" and "the dearest" are never the same line by accident.
const general: AnsweredTicketType = {
  id: "tt-general",
  name: "General",
  price_cents: 1000,
  sort_order: 0,
  ticket_questions: [size, meal],
};
const vip: AnsweredTicketType = {
  id: "tt-vip",
  name: "VIP",
  price_cents: 5000,
  sort_order: 1,
  ticket_questions: [diet],
};
const free: AnsweredTicketType = {
  id: "tt-free",
  name: "Free",
  price_cents: 0,
  sort_order: 2,
  ticket_questions: [size],
};

const NOW = new Date("2026-10-04T12:00:00Z");

function form(overrides: Partial<NamedTicketsForm> = {}): NamedTicketsForm {
  return {
    requiresNamedTickets: true,
    ticketAssignmentEnabled: true,
    startsAt: "2026-11-01T20:00:00Z",
    now: NOW,
    ticketTypes: [general, vip],
    quantities: {},
    surrenderedTicketTypeId: null,
    holders: {},
    answers: {},
    ...overrides,
  };
}

// --- When the requirement binds ---------------------------------------------

test("namedTicketsApply binds only with the setting on, assignment open and the doors shut", () => {
  const on = { requiresNamedTickets: true, ticketAssignmentEnabled: true, now: NOW };
  assert.equal(namedTicketsApply({ ...on, startsAt: "2026-11-01T20:00:00Z" }), true);
  assert.equal(namedTicketsApply({ ...on, startsAt: null }), true, "an Event with no start has not started");
  assert.equal(
    namedTicketsApply({ ...on, requiresNamedTickets: false, startsAt: null }),
    false,
    "the setting is off",
  );
  assert.equal(
    namedTicketsApply({ ...on, ticketAssignmentEnabled: false, startsAt: null }),
    false,
    "Ticket Assignment is dark",
  );
});

// The server's `now.Before(startsAt)`: the instant of the start is already
// after it, so the requirement falls silent at the doors and not a moment
// later.
test("namedTicketsApply falls silent from the instant the Event starts", () => {
  const on = { requiresNamedTickets: true, ticketAssignmentEnabled: true };
  assert.equal(namedTicketsApply({ ...on, startsAt: "2026-10-04T12:00:00Z", now: NOW }), false);
  assert.equal(namedTicketsApply({ ...on, startsAt: "2026-10-04T11:00:00Z", now: NOW }), false);
  assert.equal(namedTicketsApply({ ...on, startsAt: "2026-10-04T12:00:01Z", now: NOW }), true);
});

test("namedTicketsApply does not bind on a start it cannot read", () => {
  assert.equal(
    namedTicketsApply({
      requiresNamedTickets: true,
      ticketAssignmentEnabled: true,
      startsAt: "not a date",
      now: NOW,
    }),
    false,
  );
});

test("namedTicketsOwed owes nothing once the Event has started", () => {
  const owed = namedTicketsOwed(
    form({ startsAt: "2026-10-01T20:00:00Z", quantities: { "tt-general": 3 } }),
  );
  assert.deepEqual(owed, []);
});

test("namedTicketsOwed owes nothing with the setting off or assignment dark", () => {
  assert.deepEqual(
    namedTicketsOwed(form({ requiresNamedTickets: false, quantities: { "tt-general": 3 } })),
    [],
  );
  assert.deepEqual(
    namedTicketsOwed(form({ ticketAssignmentEnabled: false, quantities: { "tt-general": 3 } })),
    [],
  );
});

// --- The Self-held seat, mirrored from SelfHeldSeatOf ------------------------
// The cases are self_held_test.go's, as far as this page can state them.

test("selfHeldSeat is the first Ticket of the dearest line", () => {
  const catalog = [
    { id: "general", name: "General", price_cents: 1000, sort_order: 0 },
    { id: "vip", name: "VIP", price_cents: 5000, sort_order: 1 },
    { id: "free", name: "Free", price_cents: 0, sort_order: 2 },
  ];
  assert.deepEqual(selfHeldSeat(catalog, { general: 2, vip: 1, free: 3 }, null), {
    ticketTypeId: "vip",
    index: 1,
  });
});

test("selfHeldSeat breaks a tie on price by the catalog's order", () => {
  const catalog = [
    { id: "early-bird", name: "Early bird", price_cents: 2000, sort_order: 0 },
    { id: "regular", name: "Regular", price_cents: 2000, sort_order: 1 },
  ];
  assert.deepEqual(selfHeldSeat(catalog, { regular: 1, "early-bird": 1 }, null), {
    ticketTypeId: "early-bird",
    index: 1,
  });
});

/** Every ordering of `items`, so a case can prove the order it arrives in never decides. */
function permutations<T>(items: T[]): T[][] {
  if (items.length <= 1) return [items];
  return items.flatMap((item, at) =>
    permutations([...items.slice(0, at), ...items.slice(at + 1)]).map((rest) => [item, ...rest]),
  );
}

// The server breaks a tie the catalog cannot on the Ticket Type's id, so the
// seat is the same however the lines arrive (self_held_test.go's
// TestSelfHeldSeatIsTheSameWhicheverOrderTheBasketArrivesIn). The API lists
// Ticket Types by sort_order and then creation, so this page must not read a
// tie off that list: two Ticket Types can share a sort_order.
type SeatTieCase = {
  name: string;
  catalog: Pick<AnsweredTicketType, "id" | "name" | "price_cents" | "sort_order">[];
  quantities: Record<string, number>;
  want: string;
};

const seatTieCases: SeatTieCase[] = [
  {
    name: "equal prices seat the buyer on the earlier catalog place",
    catalog: [
      { id: "regular", name: "Regular", price_cents: 2000, sort_order: 1 },
      { id: "early-bird", name: "Early bird", price_cents: 2000, sort_order: 0 },
    ],
    quantities: { regular: 1, "early-bird": 1 },
    want: "early-bird",
  },
  {
    name: "equal prices and catalog places fall to the name, by bytes",
    catalog: [
      { id: "beta", name: "Beta", price_cents: 3000, sort_order: 5 },
      { id: "alpha", name: "Alpha", price_cents: 3000, sort_order: 5 },
    ],
    quantities: { beta: 1, alpha: 2 },
    want: "alpha",
  },
  {
    name: "a name compares by bytes, not by locale: upper case sorts first",
    catalog: [
      { id: "lower", name: "a", price_cents: 3000, sort_order: 5 },
      { id: "upper", name: "B", price_cents: 3000, sort_order: 5 },
    ],
    quantities: { lower: 1, upper: 1 },
    want: "upper",
  },
  {
    name: "a name compares by bytes, not by UTF-16 code units",
    catalog: [
      { id: "emoji", name: "\u{1F600}", price_cents: 3000, sort_order: 5 },
      { id: "fullwidth", name: "～", price_cents: 3000, sort_order: 5 },
    ],
    quantities: { emoji: 1, fullwidth: 1 },
    want: "fullwidth",
  },
  {
    name: "same price, catalog place and name fall to the lower id",
    catalog: [
      { id: "b-twin", name: "General", price_cents: 2500, sort_order: 3 },
      { id: "a-twin", name: "General", price_cents: 2500, sort_order: 3 },
    ],
    quantities: { "b-twin": 2, "a-twin": 1 },
    want: "a-twin",
  },
  {
    name: "the id is asked only after the name",
    catalog: [
      { id: "c-other", name: "Generalísimo", price_cents: 2500, sort_order: 3 },
      { id: "b-twin", name: "General", price_cents: 2500, sort_order: 3 },
    ],
    quantities: { "c-other": 1, "b-twin": 1 },
    want: "b-twin",
  },
  {
    name: "a full tie among several lines beside a cheaper one",
    catalog: [
      { id: "cheap", name: "Cheap", price_cents: 1000, sort_order: 0 },
      { id: "c-other", name: "Generalísimo", price_cents: 2500, sort_order: 3 },
      { id: "b-twin", name: "General", price_cents: 2500, sort_order: 3 },
      { id: "a-twin", name: "General", price_cents: 2500, sort_order: 3 },
    ],
    quantities: { cheap: 4, "c-other": 1, "b-twin": 1, "a-twin": 3 },
    want: "a-twin",
  },
];

for (const tc of seatTieCases) {
  test(`selfHeldSeat in any order: ${tc.name}`, () => {
    for (const catalog of permutations(tc.catalog)) {
      assert.deepEqual(
        selfHeldSeat(catalog, tc.quantities, null),
        { ticketTypeId: tc.want, index: 1 },
        `catalog listed as ${catalog.map((ticketType) => ticketType.id).join(", ")}`,
      );
    }
  });
}

// price_cents arrives already the Promotional Price with any passed-on fee in
// it, which is the buyer unit price the server ranks on.
test("selfHeldSeat ranks a line at the price it is sold at, not its List Price", () => {
  const promotedVip = { id: "vip", name: "VIP", price_cents: 1500, sort_order: 1 };
  const regularWithFee = { id: "regular", name: "Regular", price_cents: 2120, sort_order: 0 };
  assert.deepEqual(selfHeldSeat([promotedVip, regularWithFee], { vip: 1, regular: 2 }, null), {
    ticketTypeId: "regular",
    index: 1,
  });
  const stillDearest = { id: "vip", name: "VIP", price_cents: 4500, sort_order: 1 };
  assert.deepEqual(selfHeldSeat([regularWithFee, stillDearest], { vip: 1, regular: 3 }, null), {
    ticketTypeId: "vip",
    index: 1,
  });
});

test("selfHeldSeat seats a basket of one line on its first Ticket", () => {
  assert.deepEqual(selfHeldSeat([general], { "tt-general": 4 }, null), {
    ticketTypeId: "tt-general",
    index: 1,
  });
});

test("selfHeldSeat lets a line that mints nothing claim nothing, however dear", () => {
  assert.deepEqual(selfHeldSeat([general, vip], { "tt-vip": 0, "tt-general": 1 }, null), {
    ticketTypeId: "tt-general",
    index: 1,
  });
});

test("selfHeldSeat of an empty basket is nobody", () => {
  assert.equal(selfHeldSeat([general, vip], {}, null), null);
  assert.equal(selfHeldSeat([], {}, null), null);
});

test("selfHeldSeat asks nothing of a surrendered free line", () => {
  assert.deepEqual(selfHeldSeat([free, general], { "tt-free": 1, "tt-general": 2 }, "tt-free"), {
    ticketTypeId: "tt-general",
    index: 1,
  });
  // A basket of nothing but the surrendered line is not one the server drops
  // it from, but if it were, nobody would be seated.
  assert.equal(selfHeldSeat([free], { "tt-free": 1 }, "tt-free"), null);
});

// --- The Upgrade's surrendered line ------------------------------------------

test("surrenderedTicketTypeId is the cart's free line only when the Upgrade is offered and elected", () => {
  const inBasket = { surrendered: { ticketTypeId: "tt-free", ticketTypeName: "Free" } };
  assert.equal(surrenderedTicketTypeId(inBasket, true), "tt-free");
  assert.equal(surrenderedTicketTypeId(inBasket, false), null, "not elected: both lines are bought");
  assert.equal(surrenderedTicketTypeId(null, true), null, "a stale tick on a withdrawn offer");
  assert.equal(
    surrenderedTicketTypeId({ surrendered: null }, true),
    null,
    "the free Ticket is on an earlier Sale, and the cart loses nothing",
  );
});

// --- Every Ticket of the basket ----------------------------------------------

test("namedTicketSlots lists every Ticket, the buyer's own marked, in the page's order", () => {
  const slots = namedTicketSlots([general, vip], { "tt-general": 2, "tt-vip": 1 }, null);
  assert.deepEqual(
    slots.map((slot) => [slot.ticketTypeId, slot.index, slot.selfHeld]),
    [
      ["tt-general", 1, false],
      ["tt-general", 2, false],
      ["tt-vip", 1, true],
    ],
  );
  assert.deepEqual(
    slots[0]?.questions.map((question) => question.id),
    ["q-size", "q-meal"],
    "each Ticket is asked its own Ticket Type's questions",
  );
  assert.deepEqual(slots[2]?.questions.map((question) => question.id), ["q-diet"]);
});

test("namedTicketSlots includes Ticket Types that ask nothing, since they still owe an address", () => {
  const plain: AnsweredTicketType = { id: "tt-plain", name: "Plain", price_cents: 500, sort_order: 0 };
  const slots = namedTicketSlots([plain], { "tt-plain": 2 }, null);
  assert.deepEqual(
    slots.map((slot) => [slot.index, slot.selfHeld, slot.questions.length]),
    [
      [1, true, 0],
      [2, false, 0],
    ],
  );
});

test("namedTicketSlots leaves the surrendered free line out altogether", () => {
  const slots = namedTicketSlots([free, vip], { "tt-free": 1, "tt-vip": 1 }, "tt-free");
  assert.deepEqual(
    slots.map((slot) => [slot.ticketTypeId, slot.selfHeld]),
    [["tt-vip", true]],
  );
});

// --- What the form still owes ------------------------------------------------

test("namedTicketsOwed asks every Ticket but the buyer's own for an address", () => {
  const owed = namedTicketsOwed(
    form({ ticketTypes: [{ ...general, ticket_questions: [] }], quantities: { "tt-general": 3 } }),
  );
  assert.deepEqual(owed, [
    { ticketTypeId: "tt-general", index: 2, holderEmail: "missing", missingQuestionIds: [] },
    { ticketTypeId: "tt-general", index: 3, holderEmail: "missing", missingQuestionIds: [] },
  ]);
});

test("namedTicketsOwed asks the buyer's own Ticket for its required Answers too", () => {
  const owed = namedTicketsOwed(form({ ticketTypes: [general], quantities: { "tt-general": 1 } }));
  assert.deepEqual(owed, [
    { ticketTypeId: "tt-general", index: 1, holderEmail: null, missingQuestionIds: ["q-size"] },
  ]);
});

test("namedTicketsOwed never owes an optional question", () => {
  const answers: AnswerValues = { [answerKey("tt-general", 1, "q-size")]: { option_ids: ["opt-m"] } };
  const owed = namedTicketsOwed(form({ ticketTypes: [general], quantities: { "tt-general": 1 }, answers }));
  assert.deepEqual(owed, [], "the meal question is optional and left blank");
});

test("namedTicketsOwed asks each Ticket its own Ticket Type's required questions across a mixed basket", () => {
  const owed = namedTicketsOwed(form({ quantities: { "tt-general": 1, "tt-vip": 2 } }));
  assert.deepEqual(owed, [
    { ticketTypeId: "tt-general", index: 1, holderEmail: "missing", missingQuestionIds: ["q-size"] },
    { ticketTypeId: "tt-vip", index: 1, holderEmail: null, missingQuestionIds: ["q-diet"] },
    { ticketTypeId: "tt-vip", index: 2, holderEmail: "missing", missingQuestionIds: ["q-diet"] },
  ]);
});

test("namedTicketsOwed accepts the buyer's own address and the same address twice", () => {
  const plain: AnsweredTicketType = { id: "tt-plain", name: "Plain", price_cents: 500, sort_order: 0 };
  const holders: HolderValues = {
    [holderKey("tt-plain", 2)]: "Ana@Example.com ",
    [holderKey("tt-plain", 3)]: "ana@example.com",
    [holderKey("tt-plain", 4)]: "ben@example.com",
    [holderKey("tt-plain", 5)]: "ben@example.com",
  };
  assert.deepEqual(
    namedTicketsOwed(form({ ticketTypes: [plain], quantities: { "tt-plain": 5 }, holders })),
    [],
  );
});

test("namedTicketsOwed refuses a malformed address at its Ticket", () => {
  const plain: AnsweredTicketType = { id: "tt-plain", name: "Plain", price_cents: 500, sort_order: 0 };
  const holders: HolderValues = {
    [holderKey("tt-plain", 2)]: "ana.example.com",
    [holderKey("tt-plain", 3)]: "   ",
  };
  assert.deepEqual(
    namedTicketsOwed(form({ ticketTypes: [plain], quantities: { "tt-plain": 3 }, holders })),
    [
      { ticketTypeId: "tt-plain", index: 2, holderEmail: "invalid", missingQuestionIds: [] },
      { ticketTypeId: "tt-plain", index: 3, holderEmail: "missing", missingQuestionIds: [] },
    ],
  );
});

test("namedTicketsOwed counts an Answer the server would drop as missing", () => {
  const age = { id: "q-age", label: "Age", kind: "number" as const, required: true, options: [] };
  const typed: AnsweredTicketType = { id: "tt-a", name: "A", price_cents: 100, sort_order: 0, ticket_questions: [age, size] };
  const answers: AnswerValues = {
    [answerKey("tt-a", 1, "q-age")]: { number: "twelve" },
    [answerKey("tt-a", 1, "q-size")]: { option_ids: ["opt-gone"] },
  };
  assert.deepEqual(
    namedTicketsOwed(form({ ticketTypes: [typed], quantities: { "tt-a": 1 }, answers })),
    [{ ticketTypeId: "tt-a", index: 1, holderEmail: null, missingQuestionIds: ["q-age", "q-size"] }],
  );
});

test("namedTicketsOwed asks a surrendered free line for nothing", () => {
  const paid: AnsweredTicketType = { id: "tt-paid", name: "Paid", price_cents: 2500, sort_order: 1, ticket_questions: [] };
  const elected = namedTicketsOwed(
    form({ ticketTypes: [free, paid], quantities: { "tt-free": 1, "tt-paid": 1 }, surrenderedTicketTypeId: "tt-free" }),
  );
  assert.deepEqual(elected, []);

  // Without the election it is an ordinary Ticket and owes both.
  const kept = namedTicketsOwed(form({ ticketTypes: [free, paid], quantities: { "tt-free": 1, "tt-paid": 1 } }));
  assert.deepEqual(kept, [
    { ticketTypeId: "tt-free", index: 1, holderEmail: "missing", missingQuestionIds: ["q-size"] },
  ]);
});

test("namedTicketsOwed owes nothing once everything is given", () => {
  const holders: HolderValues = { [holderKey("tt-general", 1)]: "ben@example.com" };
  const answers: AnswerValues = {
    [answerKey("tt-general", 1, "q-size")]: { option_ids: ["opt-s"] },
    [answerKey("tt-vip", 1, "q-diet")]: { text: "None" },
  };
  assert.deepEqual(
    namedTicketsOwed(form({ quantities: { "tt-general": 1, "tt-vip": 1 }, holders, answers })),
    [],
  );
});

test("namedTicketsOwed reads a required checkbox's drawn state as its Answer", () => {
  const agree = { id: "q-agree", label: "I'll bring ID", kind: "checkbox" as const, required: true, options: [] };
  const typed: AnsweredTicketType = { id: "tt-a", name: "A", price_cents: 100, sort_order: 0, ticket_questions: [agree] };
  assert.deepEqual(namedTicketsOwed(form({ ticketTypes: [typed], quantities: { "tt-a": 1 } })), []);
});

// --- The Answers' rules, mirrored from ParseAnswer ---------------------------

test("answerIsUsable mirrors the server's per-kind rules", () => {
  const q = (kind: typeof size.kind | "short_text" | "long_text" | "number" | "date" | "checkbox" | "multi_choice") => ({
    id: "q",
    label: "Q",
    kind,
    required: true,
    options: size.options,
  });
  assert.equal(answerIsUsable(q("short_text"), { text: "  L " }), true);
  assert.equal(answerIsUsable(q("short_text"), { text: "   " }), false);
  assert.equal(answerIsUsable(q("short_text"), { text: "x".repeat(201) }), false);
  assert.equal(answerIsUsable(q("long_text"), { text: "x".repeat(2000) }), true);
  assert.equal(answerIsUsable(q("long_text"), { text: "x".repeat(2001) }), false);
  // Characters, not UTF-16 units: an astral character is one.
  assert.equal(answerIsUsable(q("short_text"), { text: "😀".repeat(200) }), true);

  assert.equal(answerIsUsable(q("number"), { number: "-3.50" }), true);
  assert.equal(answerIsUsable(q("number"), { number: "3." }), false);
  assert.equal(answerIsUsable(q("number"), { number: ".5" }), false);
  assert.equal(answerIsUsable(q("number"), { number: "1e3" }), false);
  assert.equal(answerIsUsable(q("number"), { number: "1,5" }), false);
  assert.equal(answerIsUsable(q("number"), { number: "1".repeat(16) }), false);
  assert.equal(answerIsUsable(q("number"), { number: `1.${"1".repeat(7)}` }), false);

  assert.equal(answerIsUsable(q("date"), { date: "2026-02-28" }), true);
  assert.equal(answerIsUsable(q("date"), { date: "2026-02-30" }), false);
  assert.equal(answerIsUsable(q("date"), { date: "2026-9-1" }), false);

  assert.equal(answerIsUsable(q("checkbox"), { checked: false }), true);
  assert.equal(answerIsUsable(q("checkbox"), {}), false);

  assert.equal(answerIsUsable(q("single_choice"), { option_ids: ["opt-s"] }), true);
  assert.equal(answerIsUsable(q("single_choice"), { option_ids: ["opt-s", "opt-m"] }), false);
  assert.equal(answerIsUsable(q("single_choice"), { option_ids: [] }), false);
  assert.equal(answerIsUsable(q("multi_choice"), { option_ids: ["opt-m", "opt-s"] }), true);
  assert.equal(answerIsUsable(q("multi_choice"), { option_ids: ["opt-s", "opt-s"] }), false);
  assert.equal(answerIsUsable(q("multi_choice"), { option_ids: ["opt-x"] }), false);

  assert.equal(answerIsUsable(q("short_text"), undefined), false);
});

test("withDrawnCheckboxes answers an untouched required checkbox with what it shows, and nothing else", () => {
  const agree = { id: "q-agree", label: "Agree", kind: "checkbox" as const, required: true, options: [] };
  const maybe = { id: "q-maybe", label: "Maybe", kind: "checkbox" as const, required: false, options: [] };
  const typed: AnsweredTicketType = { id: "tt-a", name: "A", price_cents: 100, sort_order: 0, ticket_questions: [agree, maybe] };
  const slots = namedTicketSlots([typed], { "tt-a": 2 }, null);
  const answers: AnswerValues = { [answerKey("tt-a", 2, "q-agree")]: { checked: true } };
  assert.deepEqual(withDrawnCheckboxes(slots, answers), {
    [answerKey("tt-a", 1, "q-agree")]: { checked: false },
    [answerKey("tt-a", 2, "q-agree")]: { checked: true },
  });
});

// --- The body --------------------------------------------------------------

test("checkoutHolderBodies sends each named Ticket's address, normalised, and never the buyer's own", () => {
  const slots = namedTicketSlots([general, vip], { "tt-general": 2, "tt-vip": 1 }, null);
  const holders: HolderValues = {
    [holderKey("tt-general", 1)]: " Ben@Example.com",
    [holderKey("tt-general", 2)]: "",
    [holderKey("tt-vip", 1)]: "typed-before-the-seat-moved@example.com",
  };
  assert.deepEqual(checkoutHolderBodies(slots, holders), [
    { ticket_type_id: "tt-general", ticket_index: 1, holder_email: "ben@example.com" },
  ]);
});

test("checkoutHolderBodies ignores addresses left behind by Tickets no longer in the cart", () => {
  const slots = namedTicketSlots([general], { "tt-general": 1 }, null);
  assert.deepEqual(checkoutHolderBodies(slots, { [holderKey("tt-general", 2)]: "ben@example.com" }), []);
});

// --- The server's refusal ----------------------------------------------------

test("refusedTickets reads NAMED_TICKETS_INCOMPLETE's details onto Ticket keys", () => {
  const refused = refusedTickets({
    tickets: [
      { ticket_type_id: "tt-general", ticket_index: 2, holder_email_missing: true, missing_question_ids: [] },
      { ticket_type_id: "tt-vip", ticket_index: 1, holder_email_missing: false, missing_question_ids: ["q-diet"] },
    ],
  });
  assert.deepEqual(refused, {
    [holderKey("tt-general", 2)]: { holderEmailMissing: true, missingQuestionIds: [] },
    [holderKey("tt-vip", 1)]: { holderEmailMissing: false, missingQuestionIds: ["q-diet"] },
  });
});

test("refusedTickets reads nothing out of details it does not recognise", () => {
  assert.deepEqual(refusedTickets(undefined), {});
  assert.deepEqual(refusedTickets({ tickets: "nope" }), {});
  assert.deepEqual(
    refusedTickets({ tickets: [{ ticket_type_id: 3 }, null, { ticket_type_id: "tt", ticket_index: 1 }] }),
    { [holderKey("tt", 1)]: { holderEmailMissing: false, missingQuestionIds: [] } },
  );
});

test("holderKeyOfField points a holders[i] field error back at the Ticket it was sent for", () => {
  const sent = [
    { ticket_type_id: "tt-general", ticket_index: 2, holder_email: "a@b" },
    { ticket_type_id: "tt-vip", ticket_index: 1, holder_email: "c@d" },
  ];
  assert.equal(holderKeyOfField("holders[1].holder_email", sent), holderKey("tt-vip", 1));
  assert.equal(holderKeyOfField("holders[2].holder_email", sent), null);
  assert.equal(holderKeyOfField("customer_first_name", sent), null);
});

test("parseCheckoutHolders relays well-shaped entries and drops the rest", () => {
  assert.deepEqual(parseCheckoutHolders("nope"), []);
  assert.deepEqual(
    parseCheckoutHolders([
      { ticket_type_id: " tt-general ", ticket_index: 2, holder_email: "Ben@Example.com" },
      { ticket_type_id: "", ticket_index: 1, holder_email: "a@b" },
      { ticket_type_id: "tt", ticket_index: 1.5, holder_email: "a@b" },
      { ticket_type_id: "tt", ticket_index: 1, holder_email: 7 },
      null,
    ]),
    [{ ticket_type_id: "tt-general", ticket_index: 2, holder_email: "Ben@Example.com" }],
  );
});

test("owedQuestionIds is a slot's required questions without a usable Answer, in the order asked", () => {
  const slot = { ticketTypeId: "tt-general", ticketTypeName: "General", index: 2, questions: [size, meal, diet] };
  assert.deepEqual(owedQuestionIds(slot, {}), ["q-size", "q-diet"]);
  assert.deepEqual(
    owedQuestionIds(slot, {
      [answerKey("tt-general", 2, "q-size")]: { option_ids: ["opt-s"] },
      // Another Ticket's reply is not this one's.
      [answerKey("tt-general", 1, "q-diet")]: { text: "None" },
    }),
    ["q-diet"],
  );
  // An Option the question does not offer is not an Answer the server keeps.
  assert.deepEqual(
    owedQuestionIds(slot, {
      [answerKey("tt-general", 2, "q-size")]: { option_ids: ["opt-xl"] },
      [answerKey("tt-general", 2, "q-diet")]: { text: "None" },
    }),
    ["q-size"],
  );
});

test("owedQuestionIds reads an existing Ticket's replies under its own id", () => {
  const slot = { ticketId: "tk-7", ticketTypeName: "General", ordinal: 3, questions: [size] };
  assert.deepEqual(owedQuestionIds(slot, {}), ["q-size"]);
  assert.deepEqual(owedQuestionIds(slot, { [answerKey("tk-7", 3, "q-size")]: { option_ids: ["opt-m"] } }), []);
});

test("questionErrorKinds says a needed question is needed, and an unusable reply only once left", () => {
  const slot = { ticketTypeId: "tt-general", ticketTypeName: "General", index: 1, questions: [size, meal, diet] };
  const sizeKey = answerKey("tt-general", 1, "q-size");
  const dietKey = answerKey("tt-general", 1, "q-diet");
  const answers: AnswerValues = { [sizeKey]: { option_ids: ["opt-xl"] }, [dietKey]: { text: "x".repeat(2001) } };

  // Nothing left and nothing needed: a half-typed reply is not told off.
  assert.deepEqual(questionErrorKinds(slot, answers, { needed: () => false, left: {} }), {});

  // Left, and unusable: invalid.
  assert.deepEqual(
    questionErrorKinds(slot, answers, { needed: () => false, left: { [sizeKey]: true, [dietKey]: true } }),
    { "q-size": "answerInvalid", "q-diet": "answerInvalid" },
  );

  // Needed wins over invalid, whatever the reply.
  assert.deepEqual(
    questionErrorKinds(slot, answers, {
      needed: (question) => question.id === "q-size",
      left: { [sizeKey]: true },
    }),
    { "q-size": "answerNeeded" },
  );

  // A blank reply left behind is not invalid: it is owed, and said elsewhere.
  assert.deepEqual(
    questionErrorKinds(slot, { [sizeKey]: { option_ids: [] } }, { needed: () => false, left: { [sizeKey]: true } }),
    {},
  );
});

test("questionErrorCopy puts each kind in the form's own words", () => {
  assert.deepEqual(
    questionErrorCopy(
      { "q-size": "answerNeeded", "q-diet": "answerInvalid" },
      { answerNeeded: "Needed.", answerInvalid: "Unusable." },
    ),
    { "q-size": "Needed.", "q-diet": "Unusable." },
  );
  assert.deepEqual(questionErrorCopy({}, { answerNeeded: "Needed.", answerInvalid: "Unusable." }), {});
});

test("questionErrorKinds hands the needed check the question and its reply", () => {
  const slot = { ticketId: "tk-7", ticketTypeName: "General", ordinal: 3, questions: [size] };
  const key = answerKey("tk-7", 3, "q-size");
  const seen: unknown[] = [];
  questionErrorKinds(slot, { [key]: { option_ids: ["opt-s"] } }, {
    needed: (question, reply) => {
      seen.push([question.id, reply]);
      return false;
    },
    left: {},
  });
  assert.deepEqual(seen, [["q-size", { option_ids: ["opt-s"] }]]);
});
