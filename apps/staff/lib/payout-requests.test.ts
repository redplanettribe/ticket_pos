import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  PAYOUT_REQUEST_STATUSES,
  RESOLUTION_REASON_MAX_LENGTH,
  TRANSFER_REFERENCE_MAX_LENGTH,
  canDecline,
  canFulfil,
  canMarkFailed,
  canMarkProcessing,
  daysWaiting,
  fulfilmentAmountDefault,
  fulfilmentDivergence,
  isCancellable,
  isOutstanding,
  payoutRequestAmountProblem,
  payoutRequestStatusToken,
  resolutionNotice,
  resolutionReasonProblem,
  transferReferenceProblem,
} from "./payout-requests.ts";

/** Both catalogs, read once, so a token can be checked for having words. */
const CATALOGS = ["en", "es"].map((locale) => {
  const catalog = JSON.parse(
    readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
  ) as { payouts: Record<string, string>; operator: Record<string, string> };
  return { locale, payouts: catalog.payouts, operator: catalog.operator };
});

// WHAT THIS FILE DOES NOT COVER, stated because the gap is easy to mistake for
// coverage. These tests exercise the helpers that DECIDE the copy; nothing here
// or anywhere else renders a component, because the repository has no
// component-testing seam and #187 deliberately did not add one (ADR 0026,
// "Consequences of the amendment"). So no test asserts that the outstanding
// card actually draws `payouts.transferSent`, that the cancel button is really
// absent while a request is `processing`, that the failure banner's button
// really opens the Payout Profile editor, or that the operator's
// mark-processing dialog really says the ledger is untouched. Each of those
// would survive a refactor that dropped it, with this file still green.

// --- the status vocabulary ------------------------------------------------

test("every status the server can send is a token this client knows", () => {
  for (const status of PAYOUT_REQUEST_STATUSES) {
    assert.equal(payoutRequestStatusToken(status), status);
  }
});

test("an unknown status is not claimed, so the caller can show it raw", () => {
  // Null rather than a fallback token: the server is the authority on which
  // states exist, and a state this client has not been taught is shown as the
  // server named it rather than as a blank badge.
  assert.equal(payoutRequestStatusToken("approved"), null);
  assert.equal(payoutRequestStatusToken(""), null);
});

// EVERY STATE HAS EXACTLY ONE WORD PER LANGUAGE, and this is what holds the
// ticket's promise: the outstanding card, the request history and the notice
// email that links to them all read one key per state, so a status cannot come
// to have two Spanish words by being drawn on two screens.
test("every status has a word in both languages, and no two states share one", () => {
  const keyFor = (status: string) =>
    `requestStatus${status[0].toUpperCase()}${status.slice(1)}`;
  for (const { locale, payouts } of CATALOGS) {
    const words = new Set<string>();
    for (const status of PAYOUT_REQUEST_STATUSES) {
      const word = payouts[keyFor(status)];
      assert.ok(word, `${locale}.json is missing payouts.${keyFor(status)}`);
      words.add(word.toLowerCase());
    }
    assert.equal(
      words.size,
      PAYOUT_REQUEST_STATUSES.length,
      `${locale}.json gives two Payout Request statuses the same word`,
    );
  }
});

// The English shim that used to be asserted here is gone with #292: the Operator
// Dashboard reads `payoutRequestStatusToken` and the same `payouts` keys the
// organizer's screens read, through `app/payout-request-status.ts`. There is now
// ONE status vocabulary, written down once, and the test above is the whole of
// what guards it.

// A FAILURE IS NOT A REFUSAL. A failure is a bank sending the money back; a
// decline is a person saying no. The two states share a column (#182) and an
// organizer with a typo in an account number must not read that the platform
// judged and rejected them (ADR 0026 amendment) — in EITHER language. This is
// the assertion that would fail if someone ever "tidied" the two words into one,
// or reached for "Rejected"/"Rechazada" for the bank's answer.
test("failed and declined never read as the same thing, in either catalog", () => {
  const refusals = ["declin", "reject", "refus", "deni", "rechaz", "recus", "neg"];
  for (const { locale, payouts } of CATALOGS) {
    const failed = payouts.requestStatusFailed;
    assert.notEqual(failed, payouts.requestStatusDeclined);
    for (const refusal of refusals) {
      assert.ok(
        !failed.toLowerCase().includes(refusal),
        `${locale}.json: "${failed}" reads as a refusal`,
      );
    }
  }
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
  assert.equal(payoutRequestAmountProblem(null, 10_000), "not_positive");
  assert.equal(payoutRequestAmountProblem(0, 10_000), "not_positive");
  assert.equal(payoutRequestAmountProblem(-1, 10_000), "not_positive");
});

test("exactly the Payable Balance is askable; one cent above is not", () => {
  assert.equal(payoutRequestAmountProblem(10_000, 10_000), null);
  assert.equal(payoutRequestAmountProblem(10_001, 10_000), "above_payable");
});

test("a zero or negative Payable Balance is explained, never compared against", () => {
  // An Organization settled against money that had not cleared has a negative
  // Payable Balance and a positive Withdrawable one (ADR 0026). Telling it to
  // "request up to -$40.00" would be an absurdity rather than an answer, which
  // is why this is its own token with its own sentence and not a comparison.
  for (const payable of [0, -4_000]) {
    assert.equal(payoutRequestAmountProblem(1_000, payable), "nothing_cleared");
  }
});

test("every amount refusal has a sentence to be said in, in both languages", () => {
  const keys = [
    "amountProblemNotPositive",
    "amountProblemNothingCleared",
    "amountProblemAbovePayable",
  ];
  for (const { locale, payouts } of CATALOGS) {
    for (const key of keys) {
      assert.ok(payouts[key], `${locale}.json is missing payouts.${key}`);
    }
    // The cap is a number the CALLER draws, in the Organization's currency: this
    // module never sees a formatter, so the sentence must take it as an argument
    // rather than expect it baked in.
    assert.match(payouts.amountProblemAbovePayable, /\{max\}/);
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

// The age is said by an ICU plural in the catalog now, not by a helper here —
// which is what lets Spanish say "hace 12 días" without this module knowing.
// What the module owes is the number, and the catalog owes a branch for every
// count including zero: a queue row reading "0 días" instead of "Hoy" is the
// failure this asserts against.
test("both plurals can say an age of zero as a word rather than as a count", () => {
  for (const { locale, payouts, operator } of CATALOGS) {
    for (const key of ["waiting", "transferAgeNoReference", "transferAgeWithReference"]) {
      assert.ok(operator[key], `${locale}.json is missing operator.${key}`);
      assert.match(operator[key], /\{days, plural,/);
      assert.match(operator[key], /=0 \{/);
    }
    // And the queue's own row names who sent the transfer inside every branch,
    // because word order around a name is not the same in both languages.
    assert.ok(payouts.requestStatusPending, `${locale}.json is missing the status vocabulary`);
    assert.match(operator.queueTransferSent, /\{who\}/);
  }
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

test("a resolution reason is required, and bounded", () => {
  assert.equal(resolutionReasonProblem("Ask again after the show"), null);
  // Blank, missing and whitespace-only are one failure: all three reach the
  // asker as a blank, which is what requiring a reason exists to prevent.
  for (const blank of ["", "   ", "\n\t"]) {
    assert.equal(resolutionReasonProblem(blank), "missing");
  }
  assert.equal(resolutionReasonProblem("x".repeat(RESOLUTION_REASON_MAX_LENGTH)), null);
  assert.equal(resolutionReasonProblem("x".repeat(RESOLUTION_REASON_MAX_LENGTH + 1)), "too_long");
  // Trimmed before it is measured, so trailing whitespace is never what tips a
  // reason over the bound.
  assert.equal(resolutionReasonProblem(`  ${"x".repeat(RESOLUTION_REASON_MAX_LENGTH)}  `), null);
});

// A DECLINE AND A FAILURE ASK FOR THE SAME COLUMN AND NOT FOR THE SAME THING.
// The rule is one function above; the difference is two sentences here, and each
// has to say what the operator is being asked FOR — a judgement, or the bank's
// own words — or the two forms become interchangeable to whoever fills them.
test("the two empty-reason refusals are different sentences in both languages", () => {
  for (const { locale, operator } of CATALOGS) {
    assert.ok(operator.declineReasonRequired, `${locale}.json is missing declineReasonRequired`);
    assert.ok(operator.failureReasonRequired, `${locale}.json is missing failureReasonRequired`);
    assert.notEqual(operator.declineReasonRequired, operator.failureReasonRequired);
    // The bound is stated by the catalog with the constant as an argument, so
    // the CHECK and the copy cannot drift apart.
    assert.match(operator.reasonTooLong, /\{max\}/);
    assert.match(operator.transferReferenceTooLong, /\{max\}/);
  }
});

test("a partial fulfilment reports a direction and an unsigned difference", () => {
  // The ordinary case says nothing at all.
  assert.equal(fulfilmentDivergence(4_794, 4_794), null);
  assert.equal(fulfilmentDivergence(null, 4_794), null);

  assert.deepEqual(fulfilmentDivergence(2_000, 4_794), {
    direction: "short",
    differenceCents: 2_794,
  });
  // Unsigned in both directions: the direction carries the sign, and a sentence
  // reading "-$27.94 less" is what a caller subtracting for itself would get.
  assert.deepEqual(fulfilmentDivergence(5_000, 4_794), {
    direction: "over",
    differenceCents: 206,
  });
});

test("a partial fulfilment says what it MEANS, not just what it costs", () => {
  // The consequence is the part an operator would otherwise assume wrongly: a
  // short transfer closes the request, and the rest is a fresh ask. The
  // difference is an ICU argument because only the caller knows the currency.
  const en = CATALOGS.find((catalog) => catalog.locale === "en")!.operator;
  const es = CATALOGS.find((catalog) => catalog.locale === "es")!.operator;
  for (const operator of [en, es]) {
    assert.match(operator.fulfilmentShort, /\{amount\}/);
    assert.match(operator.fulfilmentOver, /\{amount\}/);
  }
  assert.match(en.fulfilmentShort, /can ask again for the rest/);
  assert.match(en.fulfilmentOver, /stays visible/);
  assert.match(es.fulfilmentShort, /volver a solicitar el resto/);
  assert.match(es.fulfilmentOver, /seguirá visible/);
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
  assert.equal(transferReferenceProblem("x".repeat(TRANSFER_REFERENCE_MAX_LENGTH + 1)), "too_long");
  // Measured after trimming, so trailing whitespace never tips it over.
  assert.equal(transferReferenceProblem(`  ${"x".repeat(TRANSFER_REFERENCE_MAX_LENGTH)}  `), null);
});

// --- the failure reason ---------------------------------------------------
//
// A failure reason is required for a stronger reason than a decline's: "failed"
// tells an organizer nothing, while "the account number was rejected" is also
// the instruction. That is a claim about the SENTENCE, so it is asserted against
// the catalog rather than against the shared rule above.
test("the failure reason's refusal names the bank, in both languages", () => {
  const en = CATALOGS.find((catalog) => catalog.locale === "en")!.operator;
  const es = CATALOGS.find((catalog) => catalog.locale === "es")!.operator;
  assert.match(en.failureReasonRequired, /bank/i);
  assert.match(es.failureReasonRequired, /banco/i);
});

// --- a submitted transfer, and one that bounced (#187) --------------------

test("the processing sentence cannot be said without the date the transfer was sent", () => {
  // THE DATE IS THE POINT. Without it an organizer cannot tell whether the 48
  // hours they were promised have already run out, which is the one moment the
  // sentence exists to let them recognise — so the catalog message REQUIRES the
  // date as an argument, and a caller with no date renders nothing rather than a
  // dateless reassurance (lib/format.ts answers null for an instant it cannot
  // read, which is what makes that the easy path).
  for (const { locale, payouts } of CATALOGS) {
    assert.ok(payouts.transferSent, `${locale}.json is missing payouts.transferSent`);
    assert.match(payouts.transferSent, /\{date\}/);
    assert.match(payouts.transferSent, /48/);
  }
});

test("a decline and a failure are told apart, never one sentence", () => {
  assert.deepEqual(resolutionNotice("declined", "Ask again after the show"), {
    kind: "declined",
    reason: "Ask again after the show",
  });
  assert.deepEqual(resolutionNotice("failed", "  the account number was rejected  "), {
    kind: "failed",
    reason: "the account number was rejected",
  });

  // Two kinds, two keys, two sentences — in both languages. Nothing in a failure
  // may read as a judgement the platform made about this Organization: the bank
  // acted, the money came back, nobody decided anything.
  for (const { locale, payouts } of CATALOGS) {
    assert.ok(payouts.resolutionDeclined, `${locale}.json is missing payouts.resolutionDeclined`);
    assert.ok(payouts.resolutionFailed, `${locale}.json is missing payouts.resolutionFailed`);
    assert.notEqual(payouts.resolutionDeclined, payouts.resolutionFailed);
    for (const key of ["resolutionDeclined", "resolutionFailed"]) {
      assert.match(payouts[key], /\{reason\}/);
    }
    for (const refusal of ["declin", "reject", "refus", "deni", "rechaz"]) {
      assert.ok(
        !payouts.resolutionFailed.toLowerCase().startsWith(refusal),
        `${locale}.json: a failure opens with "${refusal}", which reads as a refusal`,
      );
    }
  }
});

test("a reason with no state to explain it is not rendered under a guessed heading", () => {
  // Blank, whitespace and missing are one case: all three would render a
  // heading with nothing after it.
  for (const blank of [null, undefined, "", "   "]) {
    assert.equal(resolutionNotice("failed", blank), null);
    assert.equal(resolutionNotice("declined", blank), null);
  }
  // And a reason arriving on a state this client has not been taught is shown
  // under no heading at all rather than under a possibly wrong one.
  assert.equal(resolutionNotice("reversed", "something new"), null);
});

test("the failure's next step says the money is where it was, then how to fix it", () => {
  // The three facts the sentence carries, asserted where it now lives. The
  // balances did not move — no Payout was ever written for a transfer that came
  // back — and saying so first is what stops "failed" reading as a loss; the
  // likeliest cause is named, because an organizer told only "it failed" has
  // nothing to look at; and the way out is a fresh ask, because a failed request
  // is terminal and no retry is ever coming.
  const en = CATALOGS.find((catalog) => catalog.locale === "en")!.payouts;
  const es = CATALOGS.find((catalog) => catalog.locale === "es")!.payouts;
  assert.match(en.transferFailedNextStep, /balance is unchanged/);
  assert.match(en.transferFailedNextStep, /account number/);
  assert.match(en.transferFailedNextStep, /ask again/);
  assert.match(es.transferFailedNextStep, /saldo no cambió/);
  assert.match(es.transferFailedNextStep, /número de cuenta/);
  assert.match(es.transferFailedNextStep, /vuelva a solicitar/);
});
