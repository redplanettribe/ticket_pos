import assert from "node:assert/strict";
import test from "node:test";

import {
  customerAreaSaleHref,
  DEFAULT_DESTINATION,
  safeNext,
  SIGN_IN_PATH,
  signInHref,
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

// --- signInHref: proving who you are must not cost you your place ------------

test("the confirmation reference survives the sign-in round trip", () => {
  // The regression this function exists for. A buyer who has just paid and
  // presses "Sign in" was sent back to /checkout/success with the ref stripped
  // off — and the reference lives in that query and in no cookie, session or API
  // this app could ask, so their confirmation was simply gone.
  const href = signInHref("/checkout/success", "ref=TP-J7K2QX9M");
  assert.equal(href, "/signin?next=%2Fcheckout%2Fsuccess%3Fref%3DTP-J7K2QX9M");

  // And it survives the guard on the other side, which is where it is read back.
  const next = decodeURIComponent(new URL(href, "https://x").searchParams.get("next")!);
  assert.equal(safeNext(next), "/checkout/success?ref=TP-J7K2QX9M");
});

test("the explorer's search and filters survive too", () => {
  const href = signInHref("/", "q=jazz&city=quito");
  const next = decodeURIComponent(new URL(href, "https://x").searchParams.get("next")!);
  assert.equal(next, "/?q=jazz&city=quito");
});

test("an Event page comes back as itself", () => {
  const href = signInHref("/acme/events/gala", "");
  assert.equal(href, "/signin?next=%2Facme%2Fevents%2Fgala");
});

test("the two destinations not worth carrying are dropped", () => {
  // Where sign-in already goes, and the form pointing at itself.
  assert.equal(signInHref(DEFAULT_DESTINATION, ""), SIGN_IN_PATH);
  assert.equal(signInHref(SIGN_IN_PATH, ""), SIGN_IN_PATH);
  assert.equal(signInHref("/signin", "next=%2Ftickets"), SIGN_IN_PATH);
});

test("a query cannot smuggle back a destination that was refused", () => {
  // Compared on the path alone: "/tickets?page=2" is still the Customer Area,
  // and carrying it would be a round trip that changes nothing.
  assert.equal(signInHref("/tickets", "page=2"), SIGN_IN_PATH);
});

test("an empty path still produces a usable link", () => {
  // usePathname returns "" on some renders before the router has a route; the
  // header must not render a href that resolves against the current directory.
  assert.equal(signInHref("", ""), "/signin?next=%2F");
});

test("the next it writes is always accepted by the guard that reads it", () => {
  // The two halves are in different files and only agree by construction, so the
  // agreement is asserted rather than assumed: nothing signInHref emits may
  // degrade to the default destination on the way back in.
  for (const [path, search] of [
    ["/checkout/success", "ref=TP-ABC"],
    ["/acme/events/gala", "ref=PARTNER7"],
    ["/", "q=jazz"],
  ] as const) {
    const href = signInHref(path, search);
    const next = decodeURIComponent(new URL(href, "https://x").searchParams.get("next")!);
    assert.notEqual(safeNext(next), DEFAULT_DESTINATION);
    assert.equal(safeNext(next), next);
  }
});
