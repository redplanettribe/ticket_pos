import type { ReactNode } from "react";

/**
 * The keys the Operator Dashboard's entries are named by — see `STAFF_NAV_KEYS`
 * for why these are keys and not words. The Operator Dashboard is translated
 * alongside the rest of the application rather than left in English: it shares
 * one shell, one switcher and one session with the Organization's surface, so an
 * untranslated panel here would be an English island inside a Spanish app (ADR
 * 0041).
 */
export const OPERATOR_NAV_KEYS = [
  "overview",
  "organizations",
  "payoutRequests",
  "findSale",
  "taxInvoicing",
  "legalCenter",
] as const;

export type OperatorNavKey = (typeof OPERATOR_NAV_KEYS)[number];

export type OperatorNavEntry = {
  key: OperatorNavKey;
  href: string;
  /** See `SidebarNavItem["exact"]`. */
  exact?: boolean;
  /** See `SidebarNavItem["badge"]`. */
  badge?: ReactNode;
};

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
}): OperatorNavEntry[] {
  return [
    // Overview is the index of the surface, not its owner: without `exact` it
    // would stay lit while an operator reads a single payout request.
    // The same "/operator" the surface question asks about (`isOnOperatorSurface`),
    // asked of differently: the surface extends past this entry, the entry does not.
    { key: "overview", href: "/operator", exact: true },
    { key: "organizations", href: "/operator/organizations" },
    { key: "payoutRequests", href: "/operator/payout-requests", badge: payoutRequestBadge },
    // Named for the act, not the collection: there is no sales browser here and
    // none is planned, so the entry promises a lookup rather than a list.
    { key: "findSale", href: "/operator/sales" },
    // GONE IN #566: "Customer consent" at /operator/consent, the standalone
    // withdrawal surface (#271). That page began by finding somebody from an
    // email address, which put an address in a request line — and the act it
    // offered now hangs off the person's own consent record inside the Legal
    // Center, reached from the acceptance browsers. One entry fewer, and one
    // fewer place a data subject's address could reach a log.
    // Tax invoicing (#450, ADR 0059): the platform's own facturas to the SRI,
    // issued by hand. It sits last because it is the platform's own paperwork
    // rather than anything an Organization or a Customer is waiting on. It
    // lands on the invoices list (#454), and the Issuer page hangs beneath the
    // same /operator/invoicing subtree, so either lights this one entry.
    { key: "taxInvoicing", href: "/operator/invoicing" },
    // The Legal Center (#561, spec #556): the platform's own agreements, and the
    // one draft of each. LAST, because it is the entry nobody is waiting on —
    // an edition is written when somebody decides to write one, not because a
    // queue filled up.
    //
    // IT IS HERE AT ALL because a surface reachable only by typing its URL is
    // not a surface. /operator/consent sat unlinked for months (#271) and the
    // Legal Center is not repeating it: the alternative to a nav entry is an
    // operator editing the privacy policy in the database by hand.
    { key: "legalCenter", href: "/operator/legal" },
  ];
}
