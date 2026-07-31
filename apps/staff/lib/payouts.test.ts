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

test("outstandingPayoutRequest is null for an organization that has never asked", () => {
  assert.equal(outstandingPayoutRequest([]), null);
});

test("outstandingPayoutRequest is null when every ask has been answered", () => {
  // Paid, declined and cancelled are all final. None of them is a reason to
  // warn an operator recording the next Payout.
  const answered = [
    { id: "a", status: "paid" },
    { id: "b", status: "declined" },
    { id: "c", status: "cancelled" },
  ];
  assert.equal(outstandingPayoutRequest(answered), null);
});

test("outstandingPayoutRequest finds the pending ask among answered ones", () => {
  // The Organization detail payload is the whole history, newest first, so the
  // outstanding one is normally at the top but need not be.
  const pending = { id: "b", status: "pending" };
  const history = [{ id: "a", status: "paid" }, pending, { id: "c", status: "cancelled" }];
  assert.equal(outstandingPayoutRequest(history), pending);
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
