import assert from "node:assert/strict";
import test from "node:test";

import {
  DECLINE_REASON_MAX_LENGTH,
  daysWaiting,
  declineReasonProblem,
  fulfilmentAmountDefault,
  fulfilmentDivergence,
  isOutstanding,
  payoutRequestAmountProblem,
  payoutRequestStatusLabel,
  waitingLabel,
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

// THE DEFINITION UNDER TEST: a request is OUTSTANDING while it still awaits an
// answer, which is what occupies the Organization's single slot. Today that is
// exactly `pending`, so this test and a test of "untouched by an operator" would
// look identical — they are not the same question, and only this one widens
// (`processing` joins it in #184).
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

// --- how long the queue says an ask has waited ----------------------------

const now = new Date("2026-07-31T15:00:00Z");

test("an age in whole days, floored, never negative", () => {
  assert.equal(daysWaiting("2026-07-31T09:00:00Z", now), 0);
  // Twenty-three hours is not a day: the queue counts elapsed days, so an
  // operator reading "1 day" knows a day has genuinely passed.
  assert.equal(daysWaiting("2026-07-30T16:00:00Z", now), 0);
  assert.equal(daysWaiting("2026-07-30T14:00:00Z", now), 1);
  assert.equal(daysWaiting("2026-07-19T15:00:00Z", now), 12);
  // Clock skew putting the ask in the future is a new row, not a negative age.
  assert.equal(daysWaiting("2026-08-02T15:00:00Z", now), 0);
  // An unparseable instant is not a reason to render NaN at an operator.
  assert.equal(daysWaiting("not an instant", now), 0);
});

test("the waiting label reads as a person would say it", () => {
  assert.equal(waitingLabel("2026-07-31T09:00:00Z", now), "Today");
  assert.equal(waitingLabel("2026-07-30T14:00:00Z", now), "1 day");
  assert.equal(waitingLabel("2026-07-19T15:00:00Z", now), "12 days");
});

// --- answering an ask (#177) ----------------------------------------------

test("the fulfilment amount is pre-filled from the request, as an input value", () => {
  // Whole units and cents alike come back as a plain decimal the amount input
  // can hold and parsePriceToCents can read straight back.
  assert.equal(fulfilmentAmountDefault(4_794), "47.94");
  assert.equal(fulfilmentAmountDefault(100_000), "1000.00");
  assert.equal(fulfilmentAmountDefault(5), "0.05");
  // No currency symbol and no thousands separator: this is an input's value,
  // not a rendering, and anything else would have to be stripped back out.
  assert.ok(!/[^0-9.]/.test(fulfilmentAmountDefault(1_234_567)));
});

test("a decline reason is required, and bounded", () => {
  assert.equal(declineReasonProblem("Ask again after the show"), null);
  // Blank, missing and whitespace-only are one failure: all three reach the
  // asker as a blank, which is what requiring a reason exists to prevent.
  for (const blank of ["", "   ", "\n\t"]) {
    assert.equal(declineReasonProblem(blank), "Say why. The organization is shown this.");
  }
  assert.equal(declineReasonProblem("x".repeat(DECLINE_REASON_MAX_LENGTH)), null);
  assert.equal(
    declineReasonProblem("x".repeat(DECLINE_REASON_MAX_LENGTH + 1)),
    `Keep it under ${DECLINE_REASON_MAX_LENGTH} characters.`,
  );
  // Trimmed before it is measured, so trailing whitespace is never what tips a
  // reason over the bound.
  assert.equal(declineReasonProblem(`  ${"x".repeat(DECLINE_REASON_MAX_LENGTH)}  `), null);
});

test("a partial fulfilment says what it means, not just what it costs", () => {
  // The ordinary case says nothing at all.
  assert.equal(fulfilmentDivergence(4_794, 4_794, money), null);
  assert.equal(fulfilmentDivergence(null, 4_794, money), null);

  const short = fulfilmentDivergence(2_000, 4_794, money);
  assert.ok(short?.startsWith("$27.94 less than was asked for."));
  // The consequence, which is the part an operator would otherwise assume
  // wrongly: the request closes, and the rest is asked for again.
  assert.ok(short?.includes("can ask again for the rest"));

  const over = fulfilmentDivergence(5_000, 4_794, money);
  assert.ok(over?.startsWith("$2.06 more than was asked for."));
  assert.ok(over?.includes("stays visible"));
});
