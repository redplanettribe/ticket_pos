import assert from "node:assert/strict";
import test from "node:test";

import {
  clampQuantity,
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
