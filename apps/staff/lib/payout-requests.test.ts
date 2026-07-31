import assert from "node:assert/strict";
import test from "node:test";

import {
  isOutstanding,
  payoutRequestAmountProblem,
  payoutRequestStatusLabel,
} from "./payout-requests.ts";

const money = (cents: number) => `$${(cents / 100).toFixed(2)}`;

// --- status labels --------------------------------------------------------

test("every status the server can send has a label", () => {
  assert.equal(payoutRequestStatusLabel("pending"), "Waiting");
  assert.equal(payoutRequestStatusLabel("paid"), "Paid");
  assert.equal(payoutRequestStatusLabel("declined"), "Declined");
  assert.equal(payoutRequestStatusLabel("cancelled"), "Cancelled");
});

test("an unknown status is shown raw rather than swallowed", () => {
  assert.equal(payoutRequestStatusLabel("approved"), "approved");
});

test("only pending is outstanding: the three end states free the slot", () => {
  assert.equal(isOutstanding("pending"), true);
  for (const status of ["paid", "declined", "cancelled"]) {
    assert.equal(isOutstanding(status), false);
  }
});

// --- the cap, as the form explains it -------------------------------------

test("an unparseable, zero or negative amount is not an ask", () => {
  assert.equal(payoutRequestAmountProblem(null, 10_000, money), "Enter an amount greater than zero.");
  assert.equal(payoutRequestAmountProblem(0, 10_000, money), "Enter an amount greater than zero.");
  assert.equal(payoutRequestAmountProblem(-1, 10_000, money), "Enter an amount greater than zero.");
});

test("exactly the Payable Balance is askable; one cent above is not", () => {
  assert.equal(payoutRequestAmountProblem(10_000, 10_000, money), null);
  assert.equal(payoutRequestAmountProblem(10_001, 10_000, money), "You can request up to $100.00 right now.");
});

test("a zero or negative Payable Balance is explained, never compared against", () => {
  // An Organization settled against money that had not cleared has a negative
  // Payable Balance and a positive Withdrawable one (ADR 0026). Telling it to
  // "request up to -$40.00" would be an absurdity rather than an answer.
  for (const payable of [0, -4_000]) {
    assert.equal(
      payoutRequestAmountProblem(1_000, payable, money),
      "Nothing has cleared yet, so there is nothing to request. Sales clear overnight.",
    );
  }
});
