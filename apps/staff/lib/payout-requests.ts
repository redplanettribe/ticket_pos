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

// --- answering an ask (#177) ----------------------------------------------

/**
 * The amount the fulfilment form starts with, as the plain decimal string the
 * amount input holds: 4794 → "47.94".
 *
 * PRE-FILLING THIS FIELD IS A DELIBERATE DEPARTURE FROM ADR 0019, which rejected
 * pre-filling an Operator Reversal's refunded amount on the grounds that a
 * pre-filled field is a field nobody reads. The distinction is whose number it
 * is. A refund amount is an assertion only the operator can make, about a
 * transfer whose size they chose; a payout amount is a figure the Organization
 * already stated and the operator agreed to by transferring it. Pre-filling a
 * number somebody else committed to is not the same as inventing one
 * (ADR 0026).
 *
 * The operator can overwrite it — an operator who transferred less types what
 * moved — and the request keeps what was asked either way.
 *
 * It is not currency-formatted: this is an input's value, and a thousands
 * separator or a symbol would have to be stripped back out before parsing.
 */
export function fulfilmentAmountDefault(amountCents: number): string {
  return (Math.round(amountCents) / 100).toFixed(2);
}

/** The bound on a decline reason, matching the column's own CHECK. */
export const DECLINE_REASON_MAX_LENGTH = 500;

/**
 * Why a decline cannot be submitted, or null when it can.
 *
 * A reason is required and the API refuses without one, so this is not a gate —
 * it is the form saying so before a round trip. Blank and whitespace-only are
 * the same failure as missing, because all three reach the asker as a blank,
 * which is the exact outcome requiring a reason exists to prevent (ADR 0026).
 */
export function declineReasonProblem(reason: string): string | null {
  const trimmed = reason.trim();
  if (!trimmed) {
    return "Say why. The organization is shown this.";
  }
  if (trimmed.length > DECLINE_REASON_MAX_LENGTH) {
    return `Keep it under ${DECLINE_REASON_MAX_LENGTH} characters.`;
  }
  return null;
}

/**
 * How a fulfilment diverges from the ask, or null when it does not.
 *
 * Partial fulfilment needs no model of its own (ADR 0026): an operator who
 * transfers less records what moved, the request goes `paid` for the smaller
 * amount, and the divergence is visible on the request forever. This is the
 * sentence that makes it visible at the moment of typing, rather than only
 * afterwards — and it says the consequence, not just the arithmetic, because
 * "the rest is not owed any more" is what an operator would otherwise assume.
 *
 * formatCents renders a cents amount in the organization's currency.
 */
export function fulfilmentDivergence(
  amountCents: number | null,
  requestedCents: number,
  formatCents: (cents: number) => string,
): string | null {
  if (amountCents === null || amountCents === requestedCents) {
    return null;
  }
  if (amountCents < requestedCents) {
    return `${formatCents(requestedCents - amountCents)} less than was asked for. The request will be marked paid for what you record, and the organization can ask again for the rest.`;
  }
  return `${formatCents(amountCents - requestedCents)} more than was asked for. The request will be marked paid, and the difference stays visible on it.`;
}
