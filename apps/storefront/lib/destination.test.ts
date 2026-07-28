import assert from "node:assert/strict";
import test from "node:test";

import {
  customerAreaSaleHref,
  DEFAULT_DESTINATION,
  safeNext,
  ticketSaleAnchorId,
} from "./destination.ts";

const SALE_ID = "11111111-2222-3333-4444-555555555555";

test("safeNext keeps an ordinary Storefront path", () => {
  assert.equal(safeNext("/rock-fest/events/summer-night"), "/rock-fest/events/summer-night");
  assert.equal(safeNext("/tickets?from=email"), "/tickets?from=email");
});

test("safeNext falls back to the Customer Area when nothing was asked for", () => {
  assert.equal(safeNext(undefined), DEFAULT_DESTINATION);
  assert.equal(safeNext(null), DEFAULT_DESTINATION);
  assert.equal(safeNext(""), DEFAULT_DESTINATION);
});

test("safeNext refuses to bounce a visitor off this Storefront", () => {
  assert.equal(safeNext("https://evil.example/steal"), DEFAULT_DESTINATION);
  assert.equal(safeNext("//evil.example/steal"), DEFAULT_DESTINATION);
  assert.equal(safeNext("javascript:alert(1)"), DEFAULT_DESTINATION);
  assert.equal(safeNext("tickets"), DEFAULT_DESTINATION);
});

// The card's DOM id and the links that aim at it are decided in one place, so a
// link can never point at an anchor no card renders.
test("customerAreaSaleHref points at one sale's card in the Customer Area", () => {
  assert.equal(customerAreaSaleHref(SALE_ID), `/tickets#${ticketSaleAnchorId(SALE_ID)}`);
  assert.equal(customerAreaSaleHref(SALE_ID), `/tickets#sale-${SALE_ID}`);
});

// Not knowing which sale is ordinary — an expired checkout-context cookie, or a
// purchase with no undo on offer — and the whole list is still where they were
// going.
test("customerAreaSaleHref falls back to the Customer Area itself", () => {
  assert.equal(customerAreaSaleHref(null), DEFAULT_DESTINATION);
  assert.equal(customerAreaSaleHref(undefined), DEFAULT_DESTINATION);
  assert.equal(customerAreaSaleHref("   "), DEFAULT_DESTINATION);
});

// A per-sale destination has to survive safeNext, or signing in would silently
// drop the buyer back onto the unanchored list.
test("safeNext keeps a per-sale fragment intact", () => {
  const href = customerAreaSaleHref(SALE_ID);
  assert.equal(safeNext(href), href);
});
