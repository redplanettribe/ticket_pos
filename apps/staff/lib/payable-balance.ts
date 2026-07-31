// The prose that explains the gap between the two balances an Organization is
// shown (#174, ADR 0025). Dependency-free so it runs directly under
// `node --test` (see payable-balance.test.ts), and so the money formatter is the
// caller's — the staff app already has one and this must not grow a second.
//
// Two numbers side by side is not the requirement. An organizer who sold tickets
// this morning and is offered less than they expect will ask why, and the whole
// reason both figures are on the page is that the answer needs both. So the gap
// gets a sentence, always, including when there is no gap.

/**
 * Explains, in one sentence, what an Organization may ask for today and why it
 * differs from what the platform owes it.
 *
 * Both figures are signed and unclamped, and the Payable Balance is never larger
 * than the Withdrawable one (cleared sales are a subset of all sales), so there
 * are exactly three cases and the third is the awkward one: an Organization
 * settled against money that had not cleared is owed a positive balance and may
 * ask for none of it. That is correct rather than a bug, and it needs saying in
 * words an organizer can act on rather than a negative number on its own.
 *
 * formatCents renders a cents amount in the Organization's currency.
 */
export function payableBalanceExplanation(
  withdrawableCents: number,
  payableCents: number,
  formatCents: (cents: number) => string,
): string {
  if (payableCents <= 0) {
    return (
      "None of your balance can be requested yet — everything that has cleared has already been paid out. " +
      "Sales from today clear tomorrow, once the day turns in Ecuador."
    );
  }
  const unclearedCents = withdrawableCents - payableCents;
  if (unclearedCents <= 0) {
    return `${formatCents(payableCents)} available to request now — your whole balance has cleared.`;
  }
  return (
    `${formatCents(payableCents)} available to request now — ` +
    `${formatCents(unclearedCents)} from today's sales clears tomorrow.`
  );
}
