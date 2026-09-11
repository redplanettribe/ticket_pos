import assert from "node:assert/strict";
import test from "node:test";

import {
  allowanceSpent,
  checkoutDestination,
  clampQuantity,
  offerableQuantity,
  parseProviderReturn,
  parseStubPaymentRequest,
  safeEventPath,
  selectionLines,
  stubOutcomeURL,
  totalCents,
  totalQuantity,
  type SellableTicketType,
} from "./checkout.ts";

const generalAdmission: SellableTicketType = {
  id: "tt-ga",
  price_cents: 3500,
  remaining: 5,
  sold_out: false,
};

const vip: SellableTicketType = {
  id: "tt-vip",
  price_cents: 8500,
  remaining: 2,
  sold_out: false,
};

const soldOut: SellableTicketType = {
  id: "tt-out",
  price_cents: 1000,
  remaining: 0,
  sold_out: true,
};

test("clampQuantity caps a stepper at what remains", () => {
  assert.equal(clampQuantity(3, generalAdmission), 3);
  assert.equal(clampQuantity(99, generalAdmission), 5);
  assert.equal(clampQuantity(3, vip), 2);
});

test("clampQuantity floors at zero and rejects nonsense", () => {
  assert.equal(clampQuantity(-1, generalAdmission), 0);
  assert.equal(clampQuantity(Number.NaN, generalAdmission), 0);
  assert.equal(clampQuantity(2.9, generalAdmission), 2);
});

test("clampQuantity never lets a sold-out Ticket Type hold a quantity", () => {
  assert.equal(clampQuantity(1, soldOut), 0);
});

test("clampQuantity never lets a closed Ticket Type hold a quantity", () => {
  // The card no longer draws a stepper for a closed Ticket Type (#607), so zero
  // is now the honest answer here: there is no control left that could put a
  // number in and no basket line the API would accept (ADR 0070). Stock is
  // beside the point — a closed Ticket Type with five left offers none of them.
  const closed: SellableTicketType = { ...generalAdmission, closed: true };
  assert.equal(clampQuantity(3, closed), 0);
  assert.equal(clampQuantity(99, closed), 0);
});

test("a Ticket Type with no Sales Cutoff clamps exactly as it did before", () => {
  // Absent is not closed, which is the state every Ticket Type is in today: the
  // feature is inert until an organizer types a date (ADR 0070).
  assert.equal(clampQuantity(3, generalAdmission), 3);
  assert.equal(clampQuantity(3, { ...generalAdmission, closed: false }), 3);
  assert.equal(clampQuantity(99, { ...generalAdmission, closed: false }), 5);
});

// --- Purchase Limit (ADR 0025) ---------------------------------------------

test("a Ticket Type with no Purchase Limit is bounded by remaining capacity alone", () => {
  // The regression that matters: unrestricted Ticket Types must offer exactly
  // what they offered before Purchase Limits existed. Absent and explicitly
  // null are the same statement.
  assert.equal(offerableQuantity(generalAdmission), 5);
  assert.equal(offerableQuantity({ ...generalAdmission, max_per_customer: null }), 5);
  assert.equal(clampQuantity(99, { ...generalAdmission, max_per_customer: null }), 5);
});

test("the Purchase Limit bounds the stepper below remaining capacity", () => {
  const rationed: SellableTicketType = { ...generalAdmission, max_per_customer: 1 };
  assert.equal(offerableQuantity(rationed), 1);
  assert.equal(clampQuantity(100, rationed), 1);
  assert.equal(clampQuantity(1, rationed), 1);
});

test("a Purchase Limit above remaining capacity is still bounded by capacity", () => {
  // A limit is not a licence to oversell: two tickets remain, so two is the
  // offer however generous the limit.
  const generous: SellableTicketType = { ...vip, max_per_customer: 10 };
  assert.equal(offerableQuantity(generous), 2);
  assert.equal(clampQuantity(10, generous), 2);
});

test("a Purchase Limit equal to remaining capacity offers all of it", () => {
  assert.equal(offerableQuantity({ ...vip, max_per_customer: 2 }), 2);
});

test("a sold-out rationed Ticket Type still offers nothing", () => {
  assert.equal(offerableQuantity({ ...soldOut, max_per_customer: 5 }), 0);
  assert.equal(clampQuantity(1, { ...soldOut, max_per_customer: 5 }), 0);
});

// --- What the signed-in Customer already holds (ADR 0025, #168) -------------

test("the offer is the Purchase Limit less what the Customer already holds", () => {
  // The case #165 left open: bounding at the bare limit offered a returning
  // Customer their whole allowance a second time.
  const cases: { name: string; limit: number; held: number | null; want: number }[] = [
    { name: "none of the allowance spent", limit: 4, held: 0, want: 4 },
    { name: "part of the allowance spent", limit: 4, held: 1, want: 3 },
    { name: "all but one spent", limit: 4, held: 3, want: 1 },
    { name: "the whole allowance spent", limit: 4, held: 4, want: 0 },
    // Lowering a Purchase Limit is not retroactive, so holding MORE than the
    // limit is a legitimate state and must offer zero, never a negative.
    { name: "more held than the limit now allows", limit: 4, held: 9, want: 0 },
    // An anonymous read discloses no holdings, and absent means unknown rather
    // than zero — so it subtracts nothing and behaves exactly as before.
    { name: "holdings unknown", limit: 4, held: null, want: 4 },
  ];

  for (const { name, limit, held, want } of cases) {
    const ticketType: SellableTicketType = {
      ...generalAdmission,
      remaining: 50,
      max_per_customer: limit,
      already_held: held,
    };
    assert.equal(offerableQuantity(ticketType), want, name);
    assert.equal(clampQuantity(99, ticketType), want, name);
  }
});

test("an absent already_held is the same statement as a null one", () => {
  // generalAdmission carries no already_held key at all, which is what a caller
  // predating #168 hands in; null is what the API sends an anonymous reader.
  const absent: SellableTicketType = { ...generalAdmission, max_per_customer: 3 };
  const explicitlyNull: SellableTicketType = { ...absent, already_held: null };
  assert.equal(offerableQuantity(absent), 3);
  assert.equal(offerableQuantity(explicitlyNull), 3);
  assert.equal(allowanceSpent(absent), false);
  assert.equal(allowanceSpent(explicitlyNull), false);
});

test("holdings on an unrestricted Ticket Type bound nothing", () => {
  // There is no allowance to spend without a Purchase Limit, so a held count
  // must not become one by subtraction.
  const unrestricted: SellableTicketType = { ...generalAdmission, already_held: 12 };
  assert.equal(offerableQuantity(unrestricted), 5);
  assert.equal(offerableQuantity({ ...unrestricted, max_per_customer: null }), 5);
  assert.equal(allowanceSpent(unrestricted), false);
});

test("capacity still binds when it is the smaller number", () => {
  // Two remain and the Customer's allowance would permit four: the Event's
  // stock wins, because an allowance is not a licence to oversell.
  const scarce: SellableTicketType = { ...vip, max_per_customer: 6, already_held: 2 };
  assert.equal(offerableQuantity(scarce), 2);
  assert.equal(clampQuantity(10, scarce), 2);
  assert.equal(allowanceSpent(scarce), false);
});

test("allowanceSpent is true only when a known Customer has used a real limit up", () => {
  const cases: { name: string; ticketType: SellableTicketType; want: boolean }[] = [
    {
      name: "allowance exactly spent",
      ticketType: { ...generalAdmission, max_per_customer: 2, already_held: 2 },
      want: true,
    },
    {
      name: "more held than the limit now allows",
      ticketType: { ...generalAdmission, max_per_customer: 2, already_held: 5 },
      want: true,
    },
    {
      name: "allowance partly spent",
      ticketType: { ...generalAdmission, max_per_customer: 2, already_held: 1 },
      want: false,
    },
    {
      name: "anonymous read of a rationed Ticket Type",
      ticketType: { ...generalAdmission, max_per_customer: 2 },
      want: false,
    },
    {
      name: "unrestricted Ticket Type",
      ticketType: { ...generalAdmission, already_held: 7 },
      want: false,
    },
    {
      // Sold out is a fact about the Event that everybody reading the page
      // sees, and it keeps its own wording: the two states must not merge.
      name: "sold out with the allowance also spent",
      ticketType: { ...soldOut, max_per_customer: 2, already_held: 2 },
      want: false,
    },
  ];

  for (const { name, ticketType, want } of cases) {
    assert.equal(allowanceSpent(ticketType), want, name);
  }
});

test("running total prices the selection per Ticket Type", () => {
  const selection = { "tt-ga": 2, "tt-vip": 1 };
  assert.equal(totalCents([generalAdmission, vip], selection), 2 * 3500 + 8500);
  assert.equal(totalQuantity(selection), 3);
});

test("an empty selection totals zero", () => {
  assert.equal(totalCents([generalAdmission, vip], {}), 0);
  assert.equal(totalQuantity({}), 0);
});

test("selectionLines drops zero quantities so a zero-quantity checkout cannot be composed", () => {
  const lines = selectionLines({ "tt-ga": 2, "tt-vip": 0 });
  assert.deepEqual(lines, [{ ticket_type_id: "tt-ga", quantity: 2 }]);
  assert.deepEqual(selectionLines({}), []);
});

test("parseStubPaymentRequest reads a well-formed stub payment URL", () => {
  const request = parseStubPaymentRequest({
    client_transaction_id: "ctid-1",
    amount_cents: "7000",
    currency: "USD",
    response_url: "http://localhost:64300/checkout/return",
  });
  assert.deepEqual(request, {
    clientTransactionId: "ctid-1",
    amountCents: 7000,
    currency: "USD",
    responseUrl: "http://localhost:64300/checkout/return",
  });
});

test("parseStubPaymentRequest refuses malformed requests", () => {
  const wellFormed = {
    client_transaction_id: "ctid-1",
    amount_cents: "7000",
    currency: "USD",
    response_url: "http://localhost:64300/checkout/return",
  };
  assert.equal(parseStubPaymentRequest({ ...wellFormed, client_transaction_id: "" }), null);
  assert.equal(parseStubPaymentRequest({ ...wellFormed, amount_cents: "70.00" }), null);
  assert.equal(parseStubPaymentRequest({ ...wellFormed, amount_cents: "-1" }), null);
  assert.equal(parseStubPaymentRequest({ ...wellFormed, amount_cents: undefined }), null);
  assert.equal(parseStubPaymentRequest({ ...wellFormed, currency: "" }), null);
  assert.equal(parseStubPaymentRequest({ ...wellFormed, response_url: "not a url" }), null);
  assert.equal(parseStubPaymentRequest({ ...wellFormed, response_url: "javascript:alert(1)" }), null);
});

test("stubOutcomeURL appends the contract params to the response URL", () => {
  const url = stubOutcomeURL("http://localhost:64300/checkout/return", "ctid-1", "approved");
  const parsed = new URL(url);
  assert.equal(parsed.pathname, "/checkout/return");
  assert.equal(parsed.searchParams.get("client_transaction_id"), "ctid-1");
  assert.equal(parsed.searchParams.get("outcome"), "approved");
});

test("stubOutcomeURL preserves query params the response URL already carried", () => {
  const url = stubOutcomeURL("http://localhost:64300/checkout/return?keep=1", "ctid-1", "declined");
  const parsed = new URL(url);
  assert.equal(parsed.searchParams.get("keep"), "1");
  assert.equal(parsed.searchParams.get("outcome"), "declined");
});

test("safeEventPath keeps a plain Storefront path and refuses anything else", () => {
  assert.equal(safeEventPath("/demo-venue/events/midnight"), "/demo-venue/events/midnight");
  assert.equal(safeEventPath("https://evil.example/x"), null);
  assert.equal(safeEventPath("//evil.example/x"), null);
  assert.equal(safeEventPath(""), null);
  assert.equal(safeEventPath(undefined), null);
});

test("parseProviderReturn reads the stub's return shape", () => {
  const { clientTransactionId, providerParams } = parseProviderReturn(
    new URLSearchParams({ client_transaction_id: "ctid-1", outcome: "approved" }),
  );
  assert.equal(clientTransactionId, "ctid-1");
  assert.deepEqual(providerParams, { client_transaction_id: "ctid-1", outcome: "approved" });
});

test("parseProviderReturn reads PayPhone's return shape and relays id verbatim", () => {
  const { clientTransactionId, providerParams } = parseProviderReturn(
    new URLSearchParams({ id: "12345", clientTransactionId: "ctid-2" }),
  );
  assert.equal(clientTransactionId, "ctid-2");
  assert.deepEqual(providerParams, { id: "12345", clientTransactionId: "ctid-2" });
});

test("parseProviderReturn yields an empty id when neither spelling is present", () => {
  const { clientTransactionId } = parseProviderReturn(new URLSearchParams({ id: "12345" }));
  assert.equal(clientTransactionId, "");
});

test("checkoutDestination sends a pending checkout to the provider's page", () => {
  assert.equal(
    checkoutDestination({
      status: "pending",
      redirect_url: "https://pay.example/hosted/abc",
    }),
    "https://pay.example/hosted/abc",
  );
});

test("checkoutDestination sends an approved checkout straight to the confirmation", () => {
  assert.equal(
    checkoutDestination({ status: "approved", confirmation_ref: "TP-2026-0042" }),
    "/checkout/success?ref=TP-2026-0042",
  );
});

test("checkoutDestination encodes the confirmation reference", () => {
  assert.equal(
    checkoutDestination({ status: "approved", confirmation_ref: "a b&c" }),
    "/checkout/success?ref=a%20b%26c",
  );
});

test("checkoutDestination refuses a checkout that named nowhere to go", () => {
  // The shape that put buyers on /{orgSlug}/events/undefined: approved with no
  // redirect_url, read as though there were one.
  assert.equal(checkoutDestination({ status: "approved" }), null);
  assert.equal(checkoutDestination({ status: "pending" }), null);
  assert.equal(checkoutDestination({ status: "pending", redirect_url: "  " }), null);
  assert.equal(checkoutDestination({}), null);
});
