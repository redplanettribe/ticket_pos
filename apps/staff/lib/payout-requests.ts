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

/**
 * The six states a Payout Request can be in. There is deliberately no `approved`
 * (ADR 0026), and `processing` is not it: it records a transfer that has already
 * been submitted to a bank, not one an operator intends to make.
 */
export const PAYOUT_REQUEST_STATUSES = [
  "pending",
  "processing",
  "paid",
  "declined",
  "cancelled",
  "failed",
] as const;

export type PayoutRequestStatus = (typeof PAYOUT_REQUEST_STATUSES)[number];

/**
 * The label a status is shown under. "Waiting" rather than "Pending" because
 * pending is the platform's word for the row's state and waiting is the
 * organizer's word for what is happening to them.
 *
 * "Failed" rather than "Rejected", and the distance from "Declined" is the whole
 * point: a decline is a judgement a person made, and a failure is a bank sending
 * the money back. An organizer with a typo in their account number must not read
 * that the platform refused them (ADR 0026 amendment).
 */
export function payoutRequestStatusLabel(status: string): string {
  switch (status) {
    case "pending":
      return "Waiting";
    case "processing":
      return "Processing";
    case "paid":
      return "Paid";
    case "declined":
      return "Declined";
    case "cancelled":
      return "Cancelled";
    case "failed":
      return "Failed";
    default:
      // A status this client has not been taught yet. Showing it raw is better
      // than showing nothing: the server is the authority on what states exist.
      return status;
  }
}

/**
 * The statuses that count as OUTSTANDING: a request still awaiting an answer,
 * occupying the single slot an Organization has.
 *
 * This is the client's one statement of that definition, and the mirror of the
 * `outstandingPayoutRequest` predicate in the sales repository, which is the
 * authority — the server enforces the slot with a partial unique index and this
 * only decides what the staff app says about it. Widening the definition is
 * adding to this list, once (#183).
 *
 * `outstanding` is NOT a synonym for `pending`. `pending` means nobody has
 * looked at the request yet; `outstanding` means it has not been answered, and a
 * `processing` request — whose transfer an operator has submitted and no bank
 * has confirmed — is emphatically not untouched and just as emphatically still
 * occupying the slot. Anything asking "may this be cancelled?" or "has an
 * operator acted?" is asking the first question and must say `pending`; the
 * per-transition gates below are exactly those questions and are written out one
 * at a time rather than against this list.
 */
const OUTSTANDING_STATUSES: readonly string[] = ["pending", "processing"];

/**
 * Whether a request is the outstanding one — the single slot an Organization
 * has, and the reason a second submission returns the first ask instead of
 * recording a new one.
 */
export function isOutstanding(status: string): boolean {
  return OUTSTANDING_STATUSES.includes(status);
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

/**
 * The bound on a decline reason, matching the column's own CHECK.
 *
 * It bounds a FAILURE's reason too, because both are the same `resolution_reason`
 * column and the same sentence to the same reader — the server states it once
 * for the same reason (resolutionReasonMaxLength in the operator handler). Two
 * constants would be two chances to drift from one CHECK.
 */
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

// --- the transfer, and the four answers an operator has (#186) --------------

/**
 * WHY THESE ARE FOUR PREDICATES AND NOT ONE.
 *
 * Until `processing` arrived, "may an operator answer this?" had a single answer
 * — the request is outstanding — and the detail page asked it once to decide
 * whether to render its form at all. It is now four different questions with
 * four different answers, and collapsing any pair of them re-creates a bug the
 * state machine exists to prevent:
 *
 *   - FULFILMENT is reachable from `pending` AND `processing`. An instant
 *     transfer skips `processing` entirely and is recorded in one step, which is
 *     the case a state describing uncertainty must not turn into a ritual; and a
 *     submitted transfer that lands is fulfilled exactly as it always was.
 *   - DECLINING is reachable from `pending` ONLY. The platform may not refuse an
 *     ask a bank is currently acting on.
 *   - MARKING PROCESSING is reachable from `pending` ONLY. A second operator
 *     pressing it is somebody about to transfer money that is already on its way.
 *   - MARKING FAILED is reachable from `processing` ONLY. A transfer nobody
 *     submitted cannot have bounced — and this is also the correction for a
 *     mis-click into `processing`, recorded with a reason saying so.
 *
 * None of these is a gate. The server guards every transition with a
 * compare-and-swap and will refuse regardless; these decide what the page
 * OFFERS, so an operator is not invited to press a button whose only possible
 * outcome is a refusal.
 */

/** Whether a Payout can be recorded against this ask. Both outstanding states. */
export function canFulfil(status: string): boolean {
  return status === "pending" || status === "processing";
}

/** Whether this ask can be refused. Untouched requests only. */
export function canDecline(status: string): boolean {
  return status === "pending";
}

/** Whether a submitted transfer can be recorded against this ask. */
export function canMarkProcessing(status: string): boolean {
  return status === "pending";
}

/** Whether a bank rejection can be recorded against this ask. */
export function canMarkFailed(status: string): boolean {
  return status === "processing";
}

/**
 * How long ago the transfer was sent, as a queue row says it: "sent today",
 * "sent 1 day ago", "sent 4 days ago".
 *
 * It reuses the ask's own age arithmetic — whole days, floored at zero — because
 * an operator triaging a backlog reads both figures in the same glance and two
 * different roundings between them would be a puzzle rather than a fact.
 *
 * IT IS NOT THE STALE FLAG AND MUST NOT BECOME ONE. This counts from the
 * VIEWER's clock; the flag is the server's answer, computed against the server's
 * clock and delivered as transfer_stale. A laptop with the wrong date should
 * garble a sentence, never decide whether a transfer is in trouble.
 */
export function transferSentLabel(submittedAt: string, now: Date = new Date()): string {
  const days = daysWaiting(submittedAt, now);
  if (days === 0) {
    return "sent today";
  }
  return days === 1 ? "sent 1 day ago" : `sent ${days} days ago`;
}

/** The bound on a transfer reference, matching the column's CHECK (migration 045). */
export const TRANSFER_REFERENCE_MAX_LENGTH = 200;

/**
 * Why a transfer reference cannot be submitted, or null when it can — INCLUDING
 * when it is blank.
 *
 * The reference is optional and its absence is ordinary: PayPhone does not
 * always hand one back synchronously, and a required field an operator cannot
 * fill is a field they will type "-" into, at which point the column holds noise
 * that looks like data. So the only thing that can be wrong with it is being too
 * long for the column.
 */
export function transferReferenceProblem(reference: string): string | null {
  if (reference.trim().length > TRANSFER_REFERENCE_MAX_LENGTH) {
    return `Keep it under ${TRANSFER_REFERENCE_MAX_LENGTH} characters.`;
  }
  return null;
}

/**
 * Why a failure reason cannot be submitted, or null when it can.
 *
 * Required, exactly as a decline's is and for a stronger reason. "Failed" tells
 * an organizer nothing; "the account number was rejected" is also the
 * instruction — go and correct the Payout Profile, because this request's copy
 * of it is frozen and the next move is a fresh ask, never a retry of this one
 * (ADR 0026 amendment). Blank and whitespace-only are the same failure as
 * missing, because all three reach the organizer as a blank.
 */
export function failureReasonProblem(reason: string): string | null {
  const trimmed = reason.trim();
  if (!trimmed) {
    return "Say what the bank said. The organization is shown this, and it is what they act on.";
  }
  if (trimmed.length > DECLINE_REASON_MAX_LENGTH) {
    return `Keep it under ${DECLINE_REASON_MAX_LENGTH} characters.`;
  }
  return null;
}
