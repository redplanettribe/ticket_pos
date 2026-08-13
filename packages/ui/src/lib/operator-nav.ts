import type { ReactNode } from "react";

import type { SidebarNavItem } from "../components/sidebar-shell";

/**
 * The Operator Dashboard's primary navigation, in the order it is read.
 *
 * The Operator Dashboard is a whole surface rather than a single screen
 * (CONTEXT.md), so it has a panel of its own that replaces the Organization's
 * — an operator crossing over is not acting for any Organization, and that
 * Organization's Dashboard, Events, Payouts and Settings are none of their
 * business here (#192).
 *
 * One entry per job the Operator Dashboard does (#193): the platform's revenue,
 * the Organizations it owes, the Payout Requests waiting to be answered, and a
 * Ticket Sale looked up by its Sale Confirmation reference. Overview leads, and
 * the work that somebody is WAITING on sits above the lookup that only answers
 * a question already being asked.
 */
export function operatorNavItems({
  payoutRequestBadge,
}: {
  /**
   * What the Payout Requests entry wears — the count of requests waiting to be
   * answered, or nothing. Handed in already decided: the nav says where the
   * queue is, not how long it is.
   */
  payoutRequestBadge?: ReactNode;
}): SidebarNavItem[] {
  return [
    // Overview is the index of the surface, not its owner: without `exact` it
    // would stay lit while an operator reads a single payout request.
    // The same "/operator" the surface question asks about (`isOnOperatorSurface`),
    // asked of differently: the surface extends past this entry, the entry does not.
    { href: "/operator", label: "Overview", exact: true },
    { href: "/operator/organizations", label: "Organizations" },
    { href: "/operator/payout-requests", label: "Payout Requests", badge: payoutRequestBadge },
    // Named for the act, not the collection: there is no sales browser here and
    // none is planned, so the entry promises a lookup rather than a list.
    { href: "/operator/sales", label: "Find a sale" },
    // The Consent Withdrawal an operator records for somebody who wrote in
    // (#271). It sits last for the same reason "Find a sale" sits above it and
    // below the queue: it answers a request that has already arrived on paper,
    // and nobody is waiting on this screen the way they wait on a payout.
    //
    // Named for the person it is about rather than for the act, because the
    // page begins with finding them: an operator holding a posted form has an
    // email address and nothing else.
    { href: "/operator/consent", label: "Customer consent" },
  ];
}
