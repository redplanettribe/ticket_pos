import type { ReactNode } from "react";

/**
 * The Operator Dashboard's primary navigation, in the order it is read.
 *
 * The Operator Dashboard is a whole surface rather than a single screen
 * (CONTEXT.md), so it has a panel of its own that replaces the Organization's
 * — an operator crossing over is not acting for any Organization, and that
 * Organization's Dashboard, Events, POS, Payouts and Settings are none of their
 * business here (#192).
 *
 * Only the destinations that exist today. Further entries arrive when the
 * dashboard is broken up into pages of its own.
 */
export type OperatorNavItem = {
  href: string;
  label: string;
  /** True for an entry that must not claim the pages beneath it. */
  exact?: boolean;
  badge?: ReactNode;
};

export function operatorNavItems({
  payoutRequestBadge,
}: {
  /**
   * What the Payout Requests entry wears — the count of requests waiting to be
   * answered, or nothing. Handed in already decided: the nav says where the
   * queue is, not how long it is.
   */
  payoutRequestBadge?: ReactNode;
}): OperatorNavItem[] {
  return [
    // Overview is the index of the surface, not its owner: without `exact` it
    // would stay lit while an operator reads a single payout request.
    { href: "/operator", label: "Overview", exact: true },
    { href: "/operator/payout-requests", label: "Payout Requests", badge: payoutRequestBadge },
  ];
}
