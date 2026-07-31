import assert from "node:assert/strict";
import test from "node:test";

import {
  exceedsWithdrawableBalance,
  formatPaidAtDate,
  outstandingPayoutRequest,
  todayISODate,
} from "./payouts.ts";

// --- paid-at rendering ----------------------------------------------------

test("formatPaidAtDate renders the calendar day, not a UTC-shifted instant", () => {
  assert.equal(formatPaidAtDate("2026-03-01"), new Date(2026, 2, 1).toLocaleDateString());
});

test("formatPaidAtDate passes a malformed value through untouched", () => {
  assert.equal(formatPaidAtDate("not-a-date"), "not-a-date");
});

// --- overdraft warning ----------------------------------------------------

test("exceedsWithdrawableBalance is false below and at the balance", () => {
  assert.equal(exceedsWithdrawableBalance(4_999, 5_000), false);
  assert.equal(exceedsWithdrawableBalance(5_000, 5_000), false);
});

test("exceedsWithdrawableBalance is true above the balance", () => {
  assert.equal(exceedsWithdrawableBalance(5_001, 5_000), true);
});

test("exceedsWithdrawableBalance flags any amount against a negative balance", () => {
  assert.equal(exceedsWithdrawableBalance(1, -2_500), true);
});

// --- paying an organization twice (#178) ----------------------------------
//
// THE DEFINITION UNDER TEST: a Payout Request is OUTSTANDING while it still
// awaits an answer, occupying the single slot an Organization has — `pending`
// and `processing` both.
//
// It is deliberately not the same question as "has an operator touched this
// yet?". The warning exists to stop an Organization being paid twice, so what it
// asks is whether the Organization is currently waiting on money.

test("outstandingPayoutRequest is null for an organization that has never asked", () => {
  assert.equal(outstandingPayoutRequest([]), null);
});

test("outstandingPayoutRequest is null when every ask has been answered", () => {
  // Paid, declined, cancelled and failed are all final. None of them is a reason
  // to warn an operator recording the next Payout — a failed transfer left no
  // money anywhere, and the Organization's next move is a fresh ask.
  const answered = [
    { id: "a", status: "paid" },
    { id: "b", status: "declined" },
    { id: "c", status: "cancelled" },
    { id: "d", status: "failed" },
  ];
  assert.equal(outstandingPayoutRequest(answered), null);
});

// THE CASE THE WARNING WAS BUILT FOR, and the one it could not see until
// `processing` existed (#186). A `pending` request is a colleague who MIGHT
// transfer; a `processing` one is a colleague who ALREADY DID, and the money may
// be hours from landing. This is where double-paying stops being theoretical.
//
// It works through isOutstanding and through nothing else — there is no second
// copy of the definition here to widen — which is why this test is three lines
// and the prefactor that made it so (#183) was the whole of the work.
//
// This is the node half of the coverage for the direct-payout warning; the Go
// half is TestOperatorDirectPayoutKeepsAProcessingRequestInTheDetailPayload in
// backend/integration/operator_direct_payout_test.go, which proves the payload
// this hangs off really carries the `processing` request.
test("outstandingPayoutRequest warns while a transfer is submitted but unconfirmed", () => {
  const submitted = { id: "b", status: "processing", amount_cents: 90_000 };
  const history = [{ id: "a", status: "paid" }, submitted];
  assert.equal(outstandingPayoutRequest(history), submitted);
});

test("outstandingPayoutRequest finds the outstanding ask among answered ones", () => {
  // The Organization detail payload is the whole history, newest first, so the
  // outstanding one is normally at the top but need not be.
  const outstanding = { id: "b", status: "pending" };
  const history = [{ id: "a", status: "paid" }, outstanding, { id: "c", status: "cancelled" }];
  assert.equal(outstandingPayoutRequest(history), outstanding);
});

test("outstandingPayoutRequest returns the request itself, so the warning can name it", () => {
  // The warning names the ask and its amount, so the helper hands back the row
  // rather than a boolean.
  const request = { id: "req-1", status: "pending", amount_cents: 90_000 };
  assert.equal(outstandingPayoutRequest([request])?.amount_cents, 90_000);
});

// --- paid-at default ------------------------------------------------------

test("todayISODate zero-pads month and day in the viewer's own zone", () => {
  assert.equal(todayISODate(new Date(2026, 0, 5, 23, 30)), "2026-01-05");
});
