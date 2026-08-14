// The gap between the two balances an Organization is shown (#174, ADR 0026).
// Dependency-free so it runs directly under `node --test` (see
// payable-balance.test.ts), and free of English so the sentence can be said in
// either language (ADR 0041).
//
// Two numbers side by side is not the requirement. An organizer who sold tickets
// this morning and is offered less than they expect will ask why, and the whole
// reason both figures are on the page is that the answer needs both. So the gap
// gets a sentence, always, including when there is no gap — and this module
// decides WHICH of the three sentences is true, while the catalog says it.

/**
 * Which of the three things is true about what may be asked for today.
 *
 * `nothing_cleared` is not "zero" and is not an error: an Organization settled
 * against money that had not cleared is owed a positive Withdrawable Balance and
 * may ask for none of it, which is correct rather than a bug, and it needs
 * saying in words an organizer can act on rather than as a negative number on
 * its own.
 */
export type PayableBalanceState = "nothing_cleared" | "all_cleared" | "some_uncleared";

/**
 * The state, and the two figures the sentence about it may name.
 *
 * Both are cents, unformatted, because the currency is the Organization's and
 * the marks around the number are the reader's — neither is a decision this
 * module is allowed to make (lib/format.ts, ADR 0041). The caller draws them and
 * hands them to `payouts.balanceAllCleared` / `payouts.balanceSomeUncleared` as
 * ICU arguments.
 *
 * `unclearedCents` is what the platform owes that has not settled yet: the
 * difference between the two balances. It is zero in the two states whose
 * sentences do not name it, rather than absent, so a caller reading it never has
 * to branch twice on the same fact.
 */
export type PayableBalance = {
  state: PayableBalanceState;
  payableCents: number;
  unclearedCents: number;
};

/**
 * What an Organization may ask for today, and why it differs from what the
 * platform owes it.
 *
 * Both figures are signed and unclamped, and the Payable Balance is never larger
 * than the Withdrawable one (cleared sales are a subset of all sales), so there
 * are exactly three cases and the third is the awkward one — see
 * `PayableBalanceState`.
 *
 * The copy this feeds must keep saying "can be requested" and never "available":
 * CONTEXT.md lists "available balance" under Payable Balance's _Avoid_, and this
 * sentence is where an organizer learns what the term means. The Spanish avoids
 * *saldo disponible* for the same reason.
 */
export function payableBalance(withdrawableCents: number, payableCents: number): PayableBalance {
  if (payableCents <= 0) {
    return { state: "nothing_cleared", payableCents, unclearedCents: 0 };
  }
  const unclearedCents = withdrawableCents - payableCents;
  if (unclearedCents <= 0) {
    return { state: "all_cleared", payableCents, unclearedCents: 0 };
  }
  return { state: "some_uncleared", payableCents, unclearedCents };
}
