// Pure Payout helpers shared by the Org Admin payouts section and the Operator
// Dashboard. Dependency-free so they run directly under `node --test`
// (see payouts.test.ts).

import { formatCalendarDay } from "./format.ts";
import { isOutstanding } from "./payout-requests.ts";

/**
 * A Payout's paid-at day, drawn in ENGLISH — the Operator Dashboard's last
 * caller of it, and nothing else (#292).
 *
 * The organizer's Payouts page no longer comes through here: it calls
 * `formatCalendarDay(payout.paid_at, locale)` itself, in the reader's Staff
 * Locale (ADR 0041). This wrapper survives only because the Operator Dashboard
 * is not translated yet, and it is deleted with the ticket that translates it.
 *
 * It used to end in a bare `toLocaleDateString()`, which followed the BROWSER's
 * locale rather than any language the application chose — so an operator on a
 * Spanish laptop already read Spanish dates on an English screen. Routing it
 * through lib/format.ts is what makes the language a decision: this surface has
 * decided English until #292 says otherwise, in one legible place.
 *
 * The calendar-day handling is unchanged and is the reason `formatCalendarDay`
 * exists: "YYYY-MM-DD" through the Date constructor is UTC midnight, which is
 * the previous day everywhere west of Greenwich, which is where this platform
 * sells.
 */
export function formatPaidAtDate(paidAt: string): string {
  return formatCalendarDay(paidAt, "en");
}

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
