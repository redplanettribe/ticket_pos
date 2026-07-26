import assert from "node:assert/strict";
import test from "node:test";

import { exceedsWithdrawableBalance, formatPaidAtDate, todayISODate } from "./payouts.ts";

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

// --- paid-at default ------------------------------------------------------

test("todayISODate zero-pads month and day in the viewer's own zone", () => {
  assert.equal(todayISODate(new Date(2026, 0, 5, 23, 30)), "2026-01-05");
});
