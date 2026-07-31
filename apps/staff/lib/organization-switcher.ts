// What the organization switcher offers a signed-in staff user, and what the
// switcher control wears while it is closed (#191). Dependency-free — no DOM,
// no fetch — so it runs directly under `node --test` (see
// organization-switcher.test.ts).

/**
 * One thing a staff user can be acting as: an Organization they are a Member
 * of, or the platform itself.
 *
 * Generic over the membership so this stays a plain data question. It decides
 * which entries exist and in what order, not how one is drawn or what a
 * Membership is made of.
 */
export type SwitcherEntry<TMembership> =
  | { kind: "organization"; membership: TMembership }
  | { kind: "platform" };

/**
 * The switcher's entries, in the order they are read.
 *
 * Platform comes last, after the Organizations, because it is the exception:
 * operator authority is orthogonal to Membership (CONTEXT.md), so it is not one
 * more Organization in the list but the other hat entirely. It appears only for
 * a session on the platform operator allowlist (ADR 0015) — the API remains the
 * actual gate; this only decides what is offered.
 *
 * A Platform Operator who is a Member of nothing gets a switcher with a single
 * entry, which is the whole reason changing hats lives here rather than on a
 * navigation item hanging off an organization context they do not have.
 */
export function switcherEntries<TMembership>({
  memberships,
  isPlatformOperator,
}: {
  memberships: readonly TMembership[];
  isPlatformOperator: boolean;
}): SwitcherEntry<TMembership>[] {
  return [
    ...memberships.map((membership) => ({ kind: "organization" as const, membership })),
    ...(isPlatformOperator ? [{ kind: "platform" as const }] : []),
  ];
}

/**
 * How many organizations are waiting to be paid, or null for nothing to show
 * (#176, ADR 0026).
 *
 * A queue's whole value is being noticed by somebody who had not already
 * decided to look — a Friday-evening request otherwise waits until an operator
 * happens to click. Absent at zero rather than shown as a "0": a badge saying
 * nothing is waiting is a badge that trains its reader to ignore it. Null means
 * the count could not be read, which is also nothing to show.
 */
export function pendingPayoutRequestBadge(count: number | null | undefined): number | null {
  return count && count > 0 ? count : null;
}
