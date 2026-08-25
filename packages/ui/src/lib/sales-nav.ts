/**
 * The keys the Sales surface's sub-tabs are named by.
 *
 * A key and not a word, for the reason `event-nav.ts` gives at length: the staff
 * app is written in two languages (ADR 0041) and @ticket-pos/ui cannot be handed
 * a `t`, because it is shared with the Storefront and the Storefront's catalog
 * is deliberately a different one. A module here that returned "Record" would be
 * returning English to a Spanish reader with nowhere to intercept it.
 *
 * So this module keeps the part that is a decision — which tabs exist, for whom,
 * and in what order — and the caller supplies the words.
 */
export const SALES_NAV_KEYS = ["sales", "record", "trends", "holderList"] as const;

export type SalesNavKey = (typeof SALES_NAV_KEYS)[number];

/**
 * One tab of the Sales surface: where it points and what it is called, with the
 * calling being a key rather than a word. The same shape `EventNavEntry` has, so
 * a caller maps one into a `PageTabsItem` the way the Event shell maps the other
 * into a `SidebarNavItem`.
 */
export type SalesNavEntry = {
  key: SalesNavKey;
  href: string;
  /** See `isNavItemActive`. */
  exact?: boolean;
};

/**
 * The Sales surface's sub-tabs, in the order they are read.
 *
 * The Sales page of an Event is a small surface of its own: the list of the
 * sales, and the tools that record one. Each is a route rather than client
 * state, so a filtered list or the recording tools can be bookmarked and sent,
 * and so a visit loads only the one being read.
 *
 * Gating is the Event's existing gating, unchanged: the list is for every Member
 * of the Event — Event Staff included — while recording a sale belongs to the
 * Org Admin and the Event Owner, who are also the only ones shown the Event's
 * money. Record is hidden from Event Staff rather than shown and refused, and
 * its route redirects them to the list, the same pair of rules Sales Trends has
 * always carried.
 *
 * Sales Trends (#460) is the third tab and carries the same owner-only gate:
 * it reads the Event's money, and it was offered from the Event panel until it
 * moved here, so that the same chart is not offered from two places.
 *
 * The Holder List (#469) is the fourth and last tab, and it followed the same
 * road out of the Event panel: the roster is a reading of the sales exactly as
 * the chart is — who those sales seat. It takes a flag of its own rather than
 * riding `fullAccess`, for the two reasons `eventNavItems` used to give: its
 * route is gated to Org Admins alone — narrower than `fullAccess`, which also
 * admits an Event Owner — and the surface exists only while EITHER Ticket
 * Assignment or Ticket Questions is open, two features that ship dark
 * (ADR 0045). Both facts are the caller's to establish, because only the caller
 * holds the Event payload the flags arrive on and the Member's actual role;
 * this module keeps only the decision that the entry exists and where it sits.
 */
export function salesNavItems({
  eventId,
  fullAccess,
  holderList = false,
}: {
  eventId: string;
  /** Whether this Member sees the owner-only tabs: an Org Admin or Event Owner. */
  fullAccess: boolean;
  /**
   * Whether this reader may see the Event's Holder List: Ticket Assignment or
   * Ticket Questions is on AND they are an Org Admin. Defaults to false, so a
   * caller that has not thought about it gets the dark state the features ship
   * in rather than a tab that 404s.
   */
  holderList?: boolean;
}): SalesNavEntry[] {
  return [
    // The list is the index of the Sales surface, not its owner: without `exact`
    // it would stay lit on every sub-tab beneath it, and a reader on Record
    // would see two tabs claiming to be the current page.
    { key: "sales", href: `/events/${eventId}/sales`, exact: true },
    ...(fullAccess ? [{ key: "record" as const, href: `/events/${eventId}/sales/record` }] : []),
    // Trends reads the sales the first tab lists, so it follows the tools that
    // record one.
    ...(fullAccess ? [{ key: "trends" as const, href: `/events/${eventId}/sales/trends` }] : []),
    // Last, after every reading of what was sold: who is coming on it. Record
    // keeps second place, where the Org Admin's most-used tool is.
    ...(holderList
      ? [{ key: "holderList" as const, href: `/events/${eventId}/sales/holders` }]
      : []),
  ];
}

/**
 * Whether a tab strip is worth drawing over these entries.
 *
 * A strip with one tab in it tells a reader nothing they did not already know
 * and suggests a choice that is not on offer, so Event Staff — who are offered
 * the list alone — get the list with no strip above it at all.
 *
 * It lives here rather than inside the renderer because it is a decision about
 * this navigation and not about how tabs are drawn; `PageTabs` renders what it
 * is handed.
 */
export function shouldDrawTabStrip(entries: readonly SalesNavEntry[]): boolean {
  return entries.length > 1;
}
