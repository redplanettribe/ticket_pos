/**
 * The keys the Reach surface's sub-tabs are named by.
 *
 * A key and not a word, for the reason `event-nav.ts` gives at length: the staff
 * app is written in two languages (ADR 0041) and @ticket-pos/ui cannot be handed
 * a `t`, because it is shared with the Storefront and the Storefront's catalog
 * is deliberately a different one. A module here that returned "Trends" would be
 * returning English to a Spanish reader with nowhere to intercept it.
 *
 * So this module keeps the part that is a decision — which tabs exist and in
 * what order — and the caller supplies the words.
 */
export const REACH_NAV_KEYS = ["trends", "affiliateLinks"] as const;

export type ReachNavKey = (typeof REACH_NAV_KEYS)[number];

/**
 * One tab of the Reach surface: where it points and what it is called, with the
 * calling being a key rather than a word. The same shape `SalesNavEntry` has, so
 * a caller maps it into a `PageTabsItem` the same way.
 */
export type ReachNavEntry = {
  key: ReachNavKey;
  href: string;
  /** See `isNavItemActive`. */
  exact?: boolean;
};

/**
 * The Reach surface's sub-tabs, in the order they are read.
 *
 * Reach (#464) is the staff surface for reading how an Event's page was reached:
 * Reach Trends first — the page's views drawn against each Affiliate Link's
 * clicks — and the Affiliate Links the page is reached with beneath. Trends
 * leads because the surface is about the page, and the links are one part of
 * that; this reverses the order the old Affiliate Links tab kept, where the
 * table came first and the chart read beneath it.
 *
 * Each tab is a route rather than client state, so the chart can be bookmarked
 * and a colleague sent straight to the links, and so a visit loads only the one
 * being read.
 *
 * No gating parameter, unlike `salesNavItems`: both tabs are for the same two
 * roles — Org Admins and Event Owners, the gate Affiliate Links has always
 * carried — and the Reach layout turns Event Staff away before the strip is
 * drawn. So there is never a one-tab strip to suppress here, and the surface as
 * a whole is either offered or not.
 */
export function reachNavItems({ eventId }: { eventId: string }): ReachNavEntry[] {
  return [
    // Trends is the index of the Reach surface, not its owner: without `exact`
    // it would stay lit on the Affiliate Links sub-tab too, and a reader there
    // would see two tabs claiming to be the current page.
    { key: "trends", href: `/events/${eventId}/reach`, exact: true },
    { key: "affiliateLinks", href: `/events/${eventId}/reach/affiliate-links` },
  ];
}
