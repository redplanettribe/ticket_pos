import assert from "node:assert/strict";
import test from "node:test";

import { signInToTicketsHref } from "./checkout-return.ts";

// The way back in for somebody who has ALREADY PAID and arrives without a
// session. Nothing here may throw and nothing may point at a page that is not
// this Storefront's own: it is the last offer made to a buyer whose money has
// already moved.

test("a buyer who comes back signed out is sent to sign in, and on to their tickets", () => {
  assert.equal(
    signInToTicketsHref("buyer@example.com"),
    "/signin?next=%2Ftickets&email=buyer%40example.com",
  );
});

test("no known address means an empty field, never a wrong one", () => {
  // A context cookie that expired, was cleared, or belongs to another browser.
  for (const email of ["", "   ", null, undefined]) {
    assert.equal(signInToTicketsHref(email), "/signin?next=%2Ftickets");
  }
});

test("only something that looks like an address reaches the field", () => {
  // The cookie is caller-controlled storage however httpOnly it is, and this
  // becomes a query parameter on a sign-in page — where an arbitrary string in
  // an input reads as something this app is asserting about the visitor.
  for (const email of ["not an address", "<script>", "a".repeat(300) + "@example.com"]) {
    assert.equal(signInToTicketsHref(email), "/signin?next=%2Ftickets");
  }
});
