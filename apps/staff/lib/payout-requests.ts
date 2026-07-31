// Pure Payout Request helpers: an Organization asking to be paid (#175,
// ADR 0026). Dependency-free — no DOM, no fetch — so they run directly under
// `node --test` (see payout-requests.test.ts).
//
// Everything here is about what the organizer is TOLD. The rules themselves are
// the server's and are re-decided there on every submission: the cap is checked
// against the Payable Balance at request time, and "one outstanding request" is
// enforced by a partial unique index. Nothing below is a gate — it is copy, and
// the point of it is that an organizer should not have to press a button to
// discover an answer the page already knows.

/** The four states a Payout Request can be in. There is deliberately no `approved` (ADR 0026). */
export const PAYOUT_REQUEST_STATUSES = ["pending", "paid", "declined", "cancelled"] as const;

export type PayoutRequestStatus = (typeof PAYOUT_REQUEST_STATUSES)[number];

/**
 * The label a status is shown under. "Waiting" rather than "Pending" because
 * pending is the platform's word for the row's state and waiting is the
 * organizer's word for what is happening to them.
 */
export function payoutRequestStatusLabel(status: string): string {
  switch (status) {
    case "pending":
      return "Waiting";
    case "paid":
      return "Paid";
    case "declined":
      return "Declined";
    case "cancelled":
      return "Cancelled";
    default:
      // A status this client has not been taught yet. Showing it raw is better
      // than showing nothing: the server is the authority on what states exist.
      return status;
  }
}

/**
 * Whether a request is the outstanding one — the single slot an Organization
 * has, and the reason a second submission returns the first ask instead of
 * recording a new one.
 */
export function isOutstanding(status: string): boolean {
  return status === "pending";
}

/**
 * How long an ask has been waiting, in whole days, floored at zero.
 *
 * The operator queue is ordered oldest first because the oldest unanswered
 * request is the one about to become a complaint (ADR 0026), and a queue that
 * says only "requested 12 March" makes every reader do that subtraction in their
 * head. Whole days rather than hours: nobody triages a payout backlog by the
 * hour, and "waiting 3 days" is the sentence an operator would actually say.
 *
 * A clock skew putting the ask in the future reads as zero rather than as a
 * negative age — the row is new, whatever the two clocks disagree about.
 */
export function daysWaiting(requestedAt: string, now: Date = new Date()): number {
  const asked = new Date(requestedAt).getTime();
  if (Number.isNaN(asked)) {
    return 0;
  }
  const days = Math.floor((now.getTime() - asked) / 86_400_000);
  return days > 0 ? days : 0;
}

/**
 * How long an ask has been waiting, as the queue says it: "Today", "1 day",
 * "12 days".
 */
export function waitingLabel(requestedAt: string, now: Date = new Date()): string {
  const days = daysWaiting(requestedAt, now);
  if (days === 0) {
    return "Today";
  }
  return days === 1 ? "1 day" : `${days} days`;
}

/**
 * Why an amount cannot be asked for, or null when it can.
 *
 * The Payable Balance is signed and may be negative — an Organization settled
 * against money that had not cleared has nothing to ask for and is owed a
 * positive Withdrawable Balance all the same (ADR 0026) — so "nothing has
 * cleared" is its own sentence rather than a comparison that reads as an
 * absurdity ("you may ask for up to -$40").
 *
 * formatCents renders a cents amount in the Organization's currency.
 */
export function payoutRequestAmountProblem(
  amountCents: number | null,
  payableBalanceCents: number,
  formatCents: (cents: number) => string,
): string | null {
  if (amountCents === null || amountCents <= 0) {
    return "Enter an amount greater than zero.";
  }
  if (payableBalanceCents <= 0) {
    return "Nothing has cleared yet, so there is nothing to request. Sales clear overnight.";
  }
  if (amountCents > payableBalanceCents) {
    return `You can request up to ${formatCents(payableBalanceCents)} right now.`;
  }
  return null;
}
