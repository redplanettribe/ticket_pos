import assert from "node:assert/strict";
import test from "node:test";

import { formatReversalDeadline } from "./format.ts";
import { signInToUndoHref, undoDeadline } from "./undo-window.ts";

// The Ticket Sale a guest surface is talking about. Unguessable and opaque: it
// addresses a card in the Customer Area and unlocks nothing on its own.
const SALE_ID = "11111111-2222-3333-4444-555555555555";

// 2026-07-08T01:00:00Z is 20:00 on 7 July in America/Guayaquil — the Reversal
// Window's cutoff on a purchase made that day, as the API reports it.
const CUTOFF = "2026-07-08T01:00:00Z";

// Read in Ecuador, the cutoff is 8:00 PM on 7 July. Read anywhere else it is
// not: in UTC it is already 1:00 AM on the 8th, and in an Event's own timezone
// it could be any hour at all. The assertion is on the date and the hour rather
// than on the whole string, because the separator between them is the runtime's
// ICU data and not this app's rule.
test("undoDeadline draws the API's instant in Ecuador time", () => {
  const label = undoDeadline({ reversible: true, reversible_until: CUTOFF });
  assert.ok(label !== null);
  assert.match(label, /Jul 7/);
  assert.match(label, /8:00\s?PM/);
});

// The guarantee the whole ticket rests on: every surface that mentions undo —
// the Customer Area's card, the checkout success page, the Confirmation Link
// page — passes the API's instant through this one function, so the hour they
// print for one sale cannot diverge.
test("undoDeadline is the same label the Customer Area's formatter produces", () => {
  assert.equal(
    undoDeadline({ reversible: true, reversible_until: CUTOFF }),
    formatReversalDeadline(CUTOFF),
  );
});

test("undoDeadline says nothing when there is no undo on offer", () => {
  // Not reversible: a sale that was never eligible, or whose window has closed.
  assert.equal(undoDeadline({ reversible: false, reversible_until: null }), null);
  // A deadline without an offer is still no offer — the page must not print a
  // countdown to an action the API would refuse.
  assert.equal(undoDeadline({ reversible: false, reversible_until: CUTOFF }), null);
  // An offer with no deadline cannot be stated, so it is not.
  assert.equal(undoDeadline({ reversible: true, reversible_until: null }), null);
  // Nothing at all: the API could not be reached, or no checkout is remembered.
  assert.equal(undoDeadline(null), null);
  assert.equal(undoDeadline(undefined), null);
});

// The buyer comes back to the sale they were thinking about, not to a list of
// everything they have ever bought (#121). The Customer Area has no per-sale
// route, so the destination is the card's own anchor on that one page.
test("signInToUndoHref sends the buyer to sign-in, bound for their own sale", () => {
  assert.equal(
    signInToUndoHref("ana@example.com", SALE_ID),
    `/signin?next=%2Ftickets%23sale-${SALE_ID}&email=ana%40example.com`,
  );
});

test("signInToUndoHref falls back to the Customer Area when no sale is known", () => {
  assert.equal(signInToUndoHref("ana@example.com", null), "/signin?next=%2Ftickets&email=ana%40example.com");
  assert.equal(signInToUndoHref("ana@example.com", undefined), "/signin?next=%2Ftickets&email=ana%40example.com");
});

test("signInToUndoHref omits an address this app does not know", () => {
  assert.equal(signInToUndoHref(null, SALE_ID), `/signin?next=%2Ftickets%23sale-${SALE_ID}`);
  assert.equal(signInToUndoHref(undefined, SALE_ID), `/signin?next=%2Ftickets%23sale-${SALE_ID}`);
  assert.equal(signInToUndoHref("   ", SALE_ID), `/signin?next=%2Ftickets%23sale-${SALE_ID}`);
});
