// Pure Payout helpers shared by the Org Admin payouts section and the Operator
// Dashboard. Dependency-free so they run directly under `node --test`
// (see payouts.test.ts).

/**
 * Renders a payout's paid-at day as the calendar date it is. Parsing
 * "YYYY-MM-DD" with the Date constructor would read it as UTC midnight and show
 * the previous day west of Greenwich, which is where this platform sells.
 */
export function formatPaidAtDate(paidAt: string): string {
  const [year, month, day] = paidAt.split("-").map(Number);
  if (!year || !month || !day) {
    return paidAt;
  }
  return new Date(year, month - 1, day).toLocaleDateString();
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
 * Today as "YYYY-MM-DD" in the viewer's own zone — the paid-at default. Built
 * from local parts rather than toISOString(), which would name yesterday for
 * anyone west of Greenwich after their local midnight.
 */
export function todayISODate(now: Date = new Date()): string {
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${now.getFullYear()}-${month}-${day}`;
}
