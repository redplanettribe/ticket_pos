// Pure Payout helpers shared by the Org Admin payouts section and the Operator
// Dashboard. Dependency-free so they run directly under `node --test`
// (see payouts.test.ts).

import { isOutstanding } from "./payout-requests.ts";

// A PAYOUT'S PAID-AT DAY is `formatCalendarDay(payout.paid_at, locale)` from
// lib/format.ts, called by the screen that draws it in the reader's Staff Locale
// (ADR 0041). The English wrapper that used to live here served the Operator
// Dashboard alone and went with #292, which translated it; before that it ended
// in a bare `toLocaleDateString()`, which followed the BROWSER's locale rather
// than any language the application chose.
//
// `formatCalendarDay` is separate from `formatDate` for a reason worth keeping
// in mind at every call site: "YYYY-MM-DD" through the Date constructor is UTC
// midnight, which is the previous day everywhere west of Greenwich, which is
// where this platform sells.

/**
 * Reports whether an amount being recorded exceeds the Organization's current
 * Withdrawable Balance. The API accepts such a Payout regardless (ADR 0015) —
 * this only decides whether the form demands an explicit confirmation first.
 * The balance is signed, so against a negative balance every amount exceeds it.
 */
export function exceedsWithdrawableBalance(amountCents: number, balanceCents: number): boolean {
  return amountCents > balanceCents;
}

/**
 * The Organization's outstanding Payout Request, or null when it is not asking
 * for anything — the other question the record-payout form asks before it
 * writes (#178, ADR 0026).
 *
 * The failure being defended against is paying an Organization TWICE: an
 * operator answering a WhatsApp thread on the Organization detail page while a
 * request for the same money sits in the queue, unaware that a colleague may be
 * about to fulfil it. That is the most expensive mistake this feature can make
 * and the hardest to undo.
 *
 * As with exceedsWithdrawableBalance above, the API accepts the Payout
 * regardless — the direct-record path is unconditional, because the bank
 * transfer already happened and an endpoint that refuses to write it down has
 * not prevented anything, it has only stopped knowing (ADR 0019, ADR 0026).
 * This only decides whether the form demands an explicit confirmation first,
 * and offers the fulfilment route as the better door.
 *
 * Nor does recording directly close anything: a $200 direct Payout and a $900
 * outstanding request are probably not the same event, so the request stays
 * outstanding and this helper will warn again next time. Blocking and
 * auto-closing are both options ADR 0026 considered and rejected.
 *
 * At most one request can be outstanding — a partial unique index says so — but
 * this takes the whole history and finds it rather than trusting the caller to
 * have filtered, because the Organization detail payload carries every ask the
 * Organization ever made, answered ones included.
 *
 * What counts as outstanding is isOutstanding's to say and this function's to
 * ask. The question here is genuinely "is this Organization waiting on money?" —
 * NOT "has anybody looked at their ask yet?" — because the mistake being
 * defended against is paying twice, and an Organization whose transfer is
 * already been submitted is the case where double-paying is concrete rather than
 * theoretical (#181). So it widens with the definition, deliberately.
 */
export function outstandingPayoutRequest<T extends { status: string }>(
  requests: readonly T[],
): T | null {
  return requests.find((request) => isOutstanding(request.status)) ?? null;
}

/**
 * Today as "YYYY-MM-DD" in the viewer's own zone — the paid-at default. Built
 * from local parts rather than toISOString(), which would name yesterday for
 * anyone west of Greenwich after their local midnight.
 */
export function todayISODate(now: Date = new Date()): string {
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${now.getFullYear()}-${month}-${day}`;
}
