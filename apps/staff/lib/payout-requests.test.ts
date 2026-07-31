import assert from "node:assert/strict";
import test from "node:test";

import {
  DECLINE_REASON_MAX_LENGTH,
  TRANSFER_REFERENCE_MAX_LENGTH,
  canDecline,
  canFulfil,
  canMarkFailed,
  canMarkProcessing,
  daysWaiting,
  declineReasonProblem,
  failureReasonProblem,
  fulfilmentAmountDefault,
  fulfilmentDivergence,
  isOutstanding,
  payoutRequestAmountProblem,
  payoutRequestStatusLabel,
  transferReferenceProblem,
  transferSentLabel,
  waitingLabel,
} from "./payout-requests.ts";

const money = (cents: number) => `$${(cents / 100).toFixed(2)}`;

// --- status labels --------------------------------------------------------

test("every status the server can send has a label", () => {
  assert.equal(payoutRequestStatusLabel("pending"), "Waiting");
  assert.equal(payoutRequestStatusLabel("processing"), "Processing");
  assert.equal(payoutRequestStatusLabel("paid"), "Paid");
  assert.equal(payoutRequestStatusLabel("declined"), "Declined");
  assert.equal(payoutRequestStatusLabel("cancelled"), "Cancelled");
  assert.equal(payoutRequestStatusLabel("failed"), "Failed");
});

// A failure is a bank sending the money back; a decline is a person saying no.
// An organizer with a typo in their account number must not read that the
// platform judged and rejected them (ADR 0026 amendment), so the two words must
// stay different words.
test("failed and declined do not share a label", () => {
  assert.notEqual(payoutRequestStatusLabel("failed"), payoutRequestStatusLabel("declined"));
});

test("an unknown status is shown raw rather than swallowed", () => {
  assert.equal(payoutRequestStatusLabel("approved"), "approved");
});

// THE DEFINITION UNDER TEST: a request is OUTSTANDING while it still awaits an
// answer, which is what occupies the Organization's single slot. It is NOT the
// same question as "has an operator touched this?" — a `processing` request has
// been touched and is still outstanding, which is the distinction the whole of
// #183 and #184 exists to keep.
test("pending and processing are outstanding: the four end states free the slot", () => {
  assert.equal(isOutstanding("pending"), true);
  // The one that matters: an in-flight transfer still occupies the slot, so the
  // Organization cannot ask again and the direct-payout warning still fires.
  assert.equal(isOutstanding("processing"), true);
  for (const status of ["paid", "declined", "cancelled", "failed"]) {
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

// --- the four answers an operator has (#186) ------------------------------
//
// These four predicates replaced one. Until `processing` existed, "may an
// operator answer this?" had a single answer and the detail page asked it once;
// it is now four questions whose answers differ, and every test below is written
// so that collapsing any pair of them fails.

test("fulfilment is reachable from BOTH pending and processing", () => {
  // The instant transfer, which skips `processing` entirely — a state describing
  // uncertainty must not become a ritual an operator clicks through.
  assert.equal(canFulfil("pending"), true);
  // And the submitted transfer that landed, recorded exactly as it always was.
  assert.equal(canFulfil("processing"), true);
  for (const status of ["paid", "declined", "cancelled", "failed"]) {
    assert.equal(canFulfil(status), false);
  }
});

test("declining is reachable from pending ONLY: the platform cannot refuse an ask the bank is acting on", () => {
  assert.equal(canDecline("pending"), true);
  // THE ASSERTION THIS FILE EXISTS FOR. `processing` is outstanding, so a gate
  // written against isOutstanding would offer a decline here — and the server
  // would refuse it, because a transfer is already out there.
  assert.equal(canDecline("processing"), false);
  for (const status of ["paid", "declined", "cancelled", "failed"]) {
    assert.equal(canDecline(status), false);
  }
});

test("marking processing is reachable from pending ONLY", () => {
  assert.equal(canMarkProcessing("pending"), true);
  // Offering it on a request whose transfer is already in flight is offering a
  // second operator the chance to send the same money twice.
  assert.equal(canMarkProcessing("processing"), false);
  for (const status of ["paid", "declined", "cancelled", "failed"]) {
    assert.equal(canMarkProcessing(status), false);
  }
});

test("marking failed is reachable from processing ONLY: nothing nobody sent can bounce", () => {
  assert.equal(canMarkFailed("processing"), true);
  assert.equal(canMarkFailed("pending"), false);
  for (const status of ["paid", "declined", "cancelled", "failed"]) {
    assert.equal(canMarkFailed(status), false);
  }
});

// The two states are answerable in ways that do not overlap, which is the
// property that makes the split load-bearing rather than cosmetic.
test("pending and processing offer different answers", () => {
  assert.equal(canDecline("pending") && !canDecline("processing"), true);
  assert.equal(canMarkFailed("processing") && !canMarkFailed("pending"), true);
});

// --- the transfer reference -----------------------------------------------

test("a blank transfer reference is not a problem: PayPhone does not always give one", () => {
  // OPTIONAL, and this is the assertion that keeps it so. A required reference
  // an operator cannot fill is a field they will type "-" into, at which point
  // the column holds noise that looks like data (ADR 0026 amendment).
  for (const blank of ["", "   ", "\n\t"]) {
    assert.equal(transferReferenceProblem(blank), null);
  }
});

test("a transfer reference is bounded at the column's own CHECK", () => {
  assert.equal(transferReferenceProblem("PP-2026-0042"), null);
  assert.equal(transferReferenceProblem("x".repeat(TRANSFER_REFERENCE_MAX_LENGTH)), null);
  assert.equal(
    transferReferenceProblem("x".repeat(TRANSFER_REFERENCE_MAX_LENGTH + 1)),
    `Keep it under ${TRANSFER_REFERENCE_MAX_LENGTH} characters.`,
  );
  // Measured after trimming, so trailing whitespace never tips it over.
  assert.equal(transferReferenceProblem(`  ${"x".repeat(TRANSFER_REFERENCE_MAX_LENGTH)}  `), null);
});

// --- the failure reason ---------------------------------------------------

test("a failure reason is required, and says what the organizer does with it", () => {
  // Required for a stronger reason than a decline's: "failed" tells an organizer
  // nothing, while "the account number was rejected" is also the instruction.
  for (const blank of ["", "   ", "\n\t"]) {
    const problem = failureReasonProblem(blank);
    assert.ok(problem);
    assert.ok(problem.includes("bank"));
  }
  assert.equal(failureReasonProblem("Account number rejected by Banco Pichincha."), null);
});

test("a failure reason shares the decline's bound, because it is the same column", () => {
  assert.equal(failureReasonProblem("x".repeat(DECLINE_REASON_MAX_LENGTH)), null);
  assert.equal(
    failureReasonProblem("x".repeat(DECLINE_REASON_MAX_LENGTH + 1)),
    `Keep it under ${DECLINE_REASON_MAX_LENGTH} characters.`,
  );
});

test("transferSentLabel reads as a sentence at every age, including today", () => {
  const submitted = "2026-07-28T12:00:00Z";
  assert.equal(transferSentLabel(submitted, new Date("2026-07-28T18:00:00Z")), "sent today");
  assert.equal(transferSentLabel(submitted, new Date("2026-07-29T13:00:00Z")), "sent 1 day ago");
  assert.equal(transferSentLabel(submitted, new Date("2026-07-31T13:00:00Z")), "sent 3 days ago");
  // A clock skew putting the submission in the future reads as today rather than
  // as a negative age.
  assert.equal(transferSentLabel(submitted, new Date("2026-07-27T12:00:00Z")), "sent today");
});
