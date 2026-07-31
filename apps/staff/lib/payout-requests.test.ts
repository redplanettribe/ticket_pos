import assert from "node:assert/strict";
import test from "node:test";

import {
  RESOLUTION_REASON_MAX_LENGTH,
  TRANSFER_FAILED_NEXT_STEP,
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
  isCancellable,
  isOutstanding,
  payoutRequestAmountProblem,
  payoutRequestStatusLabel,
  resolutionSentence,
  transferReferenceProblem,
  transferSentLabel,
  transferSentSentence,
  waitingLabel,
} from "./payout-requests.ts";

// WHAT THIS FILE DOES NOT COVER, stated because the gap is easy to mistake for
// coverage. These tests exercise the helpers that DECIDE the copy; nothing here
// or anywhere else renders a component, because the repository has no
// component-testing seam and #187 deliberately did not add one (ADR 0026,
// "Consequences of the amendment"). So no test asserts that the outstanding
// card actually shows transferSentSentence, that the cancel button is really
// absent while a request is `processing`, that the failure banner's button
// really opens the Payout Profile editor, or that the operator's
// mark-processing dialog really says the ledger is untouched. Each of those
// would survive a refactor that dropped it, with this file still green.

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

// A FAILURE IS NOT A REFUSAL. A failure is a bank sending the money back; a
// decline is a person saying no. The two states share a column (#182) and an
// organizer with a typo in an account number must not read that the platform
// judged and rejected them (ADR 0026 amendment). This is the assertion that
// would fail if someone ever "tidied" the two labels into one, or reached for
// "Rejected" for the bank's answer.
test("failed and declined never read as the same thing", () => {
  const failed = payoutRequestStatusLabel("failed");
  assert.notEqual(failed, payoutRequestStatusLabel("declined"));
  for (const refusal of ["declin", "reject", "refus", "deni"]) {
    assert.ok(!failed.toLowerCase().includes(refusal), `"${failed}" reads as a refusal`);
  }
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
  // The one that matters: a submitted, unconfirmed transfer still occupies the slot, so the
  // Organization cannot ask again and the direct-payout warning still fires.
  assert.equal(isOutstanding("processing"), true);
  // `failed` is terminal and frees the Organization to correct its details and
  // ask again. Adding it here — reasoning that an ask with no Payout behind it
  // is somehow still open — would lock an organizer out of money they are owed,
  // permanently, and this is the assertion that would catch it.
  for (const status of ["paid", "declined", "cancelled", "failed"]) {
    assert.equal(isOutstanding(status), false);
  }
});

// THE OTHER QUESTION, asked of the same row. A processing request is
// outstanding AND uncancellable: the bank is already acting on the ask, and
// withdrawing it would free the Organization to ask again for money on its way.
test("only a pending request may be cancelled", () => {
  assert.equal(isCancellable("pending"), true);
  for (const status of ["processing", "paid", "declined", "cancelled", "failed"]) {
    assert.equal(isCancellable(status), false);
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
  assert.equal(declineReasonProblem("x".repeat(RESOLUTION_REASON_MAX_LENGTH)), null);
  assert.equal(
    declineReasonProblem("x".repeat(RESOLUTION_REASON_MAX_LENGTH + 1)),
    `Keep it under ${RESOLUTION_REASON_MAX_LENGTH} characters.`,
  );
  // Trimmed before it is measured, so trailing whitespace is never what tips a
  // reason over the bound.
  assert.equal(declineReasonProblem(`  ${"x".repeat(RESOLUTION_REASON_MAX_LENGTH)}  `), null);
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
  // Offering it on a request whose transfer has already been submitted is offering a
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
  assert.equal(failureReasonProblem("x".repeat(RESOLUTION_REASON_MAX_LENGTH)), null);
  assert.equal(
    failureReasonProblem("x".repeat(RESOLUTION_REASON_MAX_LENGTH + 1)),
    `Keep it under ${RESOLUTION_REASON_MAX_LENGTH} characters.`,
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

// --- a submitted transfer, and one that bounced (#187) --------------------

const asDate = (date: Date) => date.toISOString().slice(0, 10);

test("the processing sentence names the date the transfer was sent", () => {
  const sentence = transferSentSentence("2026-07-29T18:30:00Z", asDate);
  // THE DATE IS THE POINT. Without it an organizer cannot tell whether the 48
  // hours they were promised have already run out, which is the one moment the
  // sentence exists to let them recognise.
  assert.ok(sentence?.includes("2026-07-29"), sentence ?? "no sentence");
  assert.ok(sentence?.includes("48 hours"), sentence ?? "no sentence");
});

test("no date, no sentence: a promise nobody can check is not shipped", () => {
  // A processing request always carries the instant — a CHECK ties the pair to
  // the state — so these are the shapes that mean something is wrong upstream,
  // and the honest answer is to say less rather than to reassure blindly.
  for (const missing of [null, undefined, "", "not an instant"]) {
    assert.equal(transferSentSentence(missing, asDate), null);
  }
});

test("a decline and a failure are told apart, never one sentence", () => {
  assert.equal(
    resolutionSentence("declined", "Ask again after the show"),
    "Declined: Ask again after the show",
  );

  const failure = resolutionSentence("failed", "the account number was rejected");
  assert.ok(failure?.includes("the account number was rejected"), failure ?? "no sentence");
  // Nothing in a failure may read as a judgement the platform made about this
  // Organization: the bank acted, the money came back, nobody decided anything.
  for (const refusal of ["declin", "reject", "refus", "deni"]) {
    assert.ok(
      !failure?.toLowerCase().startsWith(refusal),
      `a failure opens with "${refusal}", which reads as a refusal`,
    );
  }
  assert.notEqual(failure, resolutionSentence("declined", "the account number was rejected"));
});

test("a reason with no state to explain it is not rendered under a guessed heading", () => {
  // Blank, whitespace and missing are one case: all three would render a
  // heading with nothing after it.
  for (const blank of [null, undefined, "", "   "]) {
    assert.equal(resolutionSentence("failed", blank), null);
    assert.equal(resolutionSentence("declined", blank), null);
  }
  // And a reason arriving on a state this client has not been taught is shown
  // under no heading at all rather than under a possibly wrong one.
  assert.equal(resolutionSentence("reversed", "something new"), null);
});

test("the failure's next step says the money is where it was, then how to fix it", () => {
  // The balances did not move — no Payout was ever written for a transfer that
  // came back — and saying so first is what stops "failed" reading as a loss.
  assert.ok(TRANSFER_FAILED_NEXT_STEP.includes("balance is unchanged"));
  // The likeliest cause, named: an organizer told only "it failed" has nothing
  // to look at, and the account number is what they should look at first.
  assert.ok(TRANSFER_FAILED_NEXT_STEP.includes("account number"));
  // A failed request is terminal: the fix is a fresh ask, not a retry nobody
  // will ever make.
  assert.ok(TRANSFER_FAILED_NEXT_STEP.includes("ask again"));
});
