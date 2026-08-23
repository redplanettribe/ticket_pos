import assert from "node:assert/strict";
import test from "node:test";

import type { SellableTicketType } from "./checkout.ts";
import {
  SELECTION_PARAM,
  decodeSelection,
  encodeSelection,
  restoreSelection,
  restoreSelectionFromParam,
} from "./selection-url.ts";

/** A Ticket Type with plenty left and no Purchase Limit, unless overridden. */
function sellable(overrides: Partial<SellableTicketType> & { id: string }): SellableTicketType {
  return {
    price_cents: 1000,
    remaining: 100,
    sold_out: false,
    max_per_customer: null,
    already_held: null,
    ...overrides,
  };
}

test("SELECTION_PARAM is the one name a selection travels under", () => {
  assert.equal(SELECTION_PARAM, "sel");
});

// --- Encoding -------------------------------------------------------------

test("encodeSelection spells a selection as Ticket Type and quantity", () => {
  assert.equal(encodeSelection({ "tt-general": 2 }), "tt-general:2");
});

test("encodeSelection orders by Ticket Type id, so one basket is one string", () => {
  assert.equal(
    encodeSelection({ zebra: 1, alpha: 3 }),
    encodeSelection({ alpha: 3, zebra: 1 }),
  );
  assert.equal(encodeSelection({ zebra: 1, alpha: 3 }), "alpha:3,zebra:1");
});

test("encodeSelection drops what is not a chosen quantity", () => {
  assert.equal(encodeSelection({}), "");
  assert.equal(encodeSelection({ alpha: 0 }), "");
  assert.equal(encodeSelection({ alpha: -2 }), "");
  assert.equal(encodeSelection({ alpha: Number.NaN }), "");
  assert.equal(encodeSelection({ alpha: Number.POSITIVE_INFINITY }), "");
  assert.equal(encodeSelection({ alpha: 1.5, beta: 2 }), "beta:2");
  // An id that cannot be spelled in this format is not smuggled into it.
  assert.equal(encodeSelection({ "a:b": 2 }), "");
  assert.equal(encodeSelection({ "a,b": 2 }), "");
  assert.equal(encodeSelection({ "": 2 }), "");
});

test("encodeSelection never emits a string its own decoder refuses", () => {
  const many: Record<string, number> = {};
  for (let i = 0; i < 200; i += 1) many[`tt-${String(i).padStart(3, "0")}`] = 99999;
  const encoded = encodeSelection(many);
  const decoded = decodeSelection(encoded);
  assert.ok(Object.keys(decoded).length > 0);
  assert.equal(encodeSelection(decoded), encoded);
});

// --- Round trip -----------------------------------------------------------

test("a selection of Ticket Types and quantities round-trips", () => {
  const selection = {
    "018f3c2a-1c4e-7b3a-9f21-2f5f6a7b8c9d": 2,
    "018f3c2a-1c4e-7b3a-9f21-2f5f6a7b8c9e": 1,
    ga_2026: 7,
  };
  assert.deepEqual(decodeSelection(encodeSelection(selection)), selection);
});

test("a selection round-trips through a real query string", () => {
  const selection = { alpha: 3, beta: 1 };
  const params = new URLSearchParams();
  params.set(SELECTION_PARAM, encodeSelection(selection));
  const back = new URLSearchParams(params.toString()).get(SELECTION_PARAM);
  assert.deepEqual(decodeSelection(back), selection);
});

// --- Decoding: hostile, malformed, truncated, empty ------------------------

test("decodeSelection reads a well-formed selection", () => {
  assert.deepEqual(decodeSelection("alpha:2,beta:1"), { alpha: 2, beta: 1 });
  assert.deepEqual(decodeSelection("  alpha:2  "), { alpha: 2 });
});

test("decodeSelection yields an empty selection for anything that is not one", () => {
  for (const raw of [
    null,
    undefined,
    "",
    "   ",
    // Truncated, in each of the places a string can be cut.
    "alpha:",
    "alpha",
    ":2",
    "alpha:2,",
    ",alpha:2",
    "alpha:2,beta",
    "alpha:2,,beta:1",
    // Not a quantity.
    "alpha:0",
    "alpha:-1",
    "alpha:1.5",
    "alpha:two",
    "alpha:1e3",
    "alpha:+2",
    "alpha: 2",
    "alpha:02",
    "alpha:99999",
    "alpha:Infinity",
    "alpha:NaN",
    // A second field appended to a format that has two.
    "alpha:2:3",
    // A duplicate is two statements about one Ticket Type, and there is no
    // honest way to pick between them.
    "alpha:2,alpha:3",
    "alpha:2,alpha:2",
    // Ids that are addresses, paths or protocols rather than identifiers.
    "https://evil.example:2",
    "../../alpha:2",
    "alpha/beta:2",
    "alpha beta:2",
    "<script>:2",
    "__proto__:2",
    "constructor:2",
    // Structural absurdity: not a basket anybody built.
    `${"a".repeat(65)}:2`,
    `alpha:2,${"x".repeat(4000)}:1`,
    Array.from({ length: 60 }, (_, i) => `tt${i}:1`).join(","),
    // A repeated query parameter is two selections, which is no selection.
    ["alpha:2", "beta:1"],
  ] as (string | string[] | null | undefined)[]) {
    assert.deepEqual(decodeSelection(raw), {}, `expected ${String(raw)} to decode to nothing`);
  }
});

test("decodeSelection never throws, whatever it is handed", () => {
  const nasty = [
    "%%%",
    "\0:2",
    "\uD800:2",
    "alpha:٢",
    "alpha:²",
    "alpha:2\n,beta:1",
    "alpha:2;beta:1",
    "alpha%3A2",
  ];
  for (const raw of nasty) {
    assert.doesNotThrow(() => decodeSelection(raw), `threw on ${raw}`);
    assert.deepEqual(decodeSelection(raw), {});
  }
});

// --- Re-judging on arrival -------------------------------------------------

test("restoreSelection honours a selection that still fits", () => {
  const ticketTypes = [sellable({ id: "alpha" }), sellable({ id: "beta" })];
  const restored = restoreSelection(ticketTypes, { alpha: 2, beta: 1 });
  assert.deepEqual(restored.selection, { alpha: 2, beta: 1 });
  assert.deepEqual(restored.adjustments, []);
});

test("restoreSelection ignores an unknown Ticket Type without error", () => {
  const ticketTypes = [sellable({ id: "alpha" })];
  const restored = restoreSelection(ticketTypes, { alpha: 1, ghost: 4 });
  assert.deepEqual(restored.selection, { alpha: 1 });
  // Nothing is reported: we cannot name a Ticket Type this Event does not have,
  // and a forged id is not news the buyer can act on.
  assert.deepEqual(restored.adjustments, []);
});

test("restoreSelection drops a Ticket Type that has sold out, and says so", () => {
  const ticketTypes = [sellable({ id: "alpha", sold_out: true, remaining: 0 })];
  const restored = restoreSelection(ticketTypes, { alpha: 2 });
  assert.deepEqual(restored.selection, {});
  assert.deepEqual(restored.adjustments, [
    { ticketTypeId: "alpha", requested: 2, restored: 0, reason: "sold_out" },
  ]);
});

test("restoreSelection reduces a quantity to what capacity allows, and says so", () => {
  const ticketTypes = [sellable({ id: "alpha", remaining: 1 })];
  const restored = restoreSelection(ticketTypes, { alpha: 4 });
  assert.deepEqual(restored.selection, { alpha: 1 });
  assert.deepEqual(restored.adjustments, [
    { ticketTypeId: "alpha", requested: 4, restored: 1, reason: "capacity" },
  ]);
});

test("restoreSelection reduces a quantity to the Purchase Limit, and says so", () => {
  // Two of a limit of three already held, so one more is all this Customer may
  // hold — capacity is not the binding rule and must not be blamed (ADR 0025).
  const ticketTypes = [
    sellable({ id: "alpha", remaining: 50, max_per_customer: 3, already_held: 2 }),
  ];
  const restored = restoreSelection(ticketTypes, { alpha: 3 });
  assert.deepEqual(restored.selection, { alpha: 1 });
  assert.deepEqual(restored.adjustments, [
    { ticketTypeId: "alpha", requested: 3, restored: 1, reason: "purchase_limit" },
  ]);
});

test("restoreSelection drops a Ticket Type whose allowance is spent, and says so", () => {
  const ticketTypes = [
    sellable({ id: "alpha", remaining: 50, max_per_customer: 2, already_held: 2 }),
  ];
  const restored = restoreSelection(ticketTypes, { alpha: 2 });
  assert.deepEqual(restored.selection, {});
  assert.deepEqual(restored.adjustments, [
    { ticketTypeId: "alpha", requested: 2, restored: 0, reason: "purchase_limit" },
  ]);
});

test("restoreSelection leaves an anonymous read bounded by capacity alone", () => {
  // already_held is null: we do not know who is asking, so the Purchase Limit
  // narrows nothing beyond itself.
  const ticketTypes = [
    sellable({ id: "alpha", remaining: 50, max_per_customer: 4, already_held: null }),
  ];
  assert.deepEqual(restoreSelection(ticketTypes, { alpha: 4 }).selection, { alpha: 4 });
  assert.deepEqual(restoreSelection(ticketTypes, { alpha: 5 }).adjustments, [
    { ticketTypeId: "alpha", requested: 5, restored: 4, reason: "purchase_limit" },
  ]);
});

test("restoreSelection reports every Ticket Type it could not restore in full", () => {
  const ticketTypes = [
    sellable({ id: "alpha", sold_out: true, remaining: 0 }),
    sellable({ id: "beta", remaining: 1 }),
    sellable({ id: "gamma" }),
  ];
  const restored = restoreSelection(ticketTypes, { alpha: 1, beta: 3, gamma: 2 });
  assert.deepEqual(restored.selection, { beta: 1, gamma: 2 });
  assert.deepEqual(
    restored.adjustments.map((a) => a.ticketTypeId),
    ["alpha", "beta"],
  );
});

test("restoreSelection carries no price, so an expired Promotion cannot be honoured", () => {
  // The encoded form has no price in it to trust, and the restored selection is
  // quantities only: whatever price_cents the Event page was just served is the
  // one the running total is built from (ADR 0021).
  const encoded = encodeSelection({ alpha: 2 });
  assert.equal(encoded, "alpha:2");
  const cheapWhilePromoted = [sellable({ id: "alpha", price_cents: 500 })];
  const fullPriceNow = [sellable({ id: "alpha", price_cents: 1000 })];
  assert.deepEqual(
    restoreSelection(cheapWhilePromoted, decodeSelection(encoded)).selection,
    restoreSelection(fullPriceNow, decodeSelection(encoded)).selection,
  );
});

test("restoreSelection of nothing is nothing, and reports nothing", () => {
  const ticketTypes = [sellable({ id: "alpha" })];
  const restored = restoreSelection(ticketTypes, {});
  assert.deepEqual(restored.selection, {});
  assert.deepEqual(restored.adjustments, []);
});

// --- Arrival, end to end ---------------------------------------------------

test("restoreSelectionFromParam restores the steppers from an address", () => {
  const ticketTypes = [sellable({ id: "alpha" }), sellable({ id: "beta", remaining: 1 })];
  const restored = restoreSelectionFromParam(ticketTypes, encodeSelection({ alpha: 2, beta: 3 }));
  assert.deepEqual(restored.selection, { alpha: 2, beta: 1 });
  assert.deepEqual(restored.adjustments, [
    { ticketTypeId: "beta", requested: 3, restored: 1, reason: "capacity" },
  ]);
});

test("arriving with no encoded selection is arriving with nothing at all", () => {
  const ticketTypes = [sellable({ id: "alpha" })];
  for (const raw of [undefined, null, "", "junk", ["a:1", "b:2"]] as (
    | string
    | string[]
    | null
    | undefined
  )[]) {
    const restored = restoreSelectionFromParam(ticketTypes, raw);
    assert.deepEqual(restored.selection, {});
    assert.deepEqual(restored.adjustments, []);
  }
});
