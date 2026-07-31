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
