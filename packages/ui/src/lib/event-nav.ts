/**
 * The keys the Event panel's entries are named by.
 *
 * A key and not a word, for the reason `staff-nav.ts` gives at length: the staff
 * app is written in two languages (ADR 0041) and @ticket-pos/ui cannot be handed
 * a `t`, because it is shared with the Storefront and the Storefront's catalog is
 * deliberately a different one. A module here that returned "Ticket Types" would
 * be returning English to a Spanish reader with nowhere to intercept it.
 *
 * So this module keeps the part that is a decision — which entries exist, for
 * whom, and in what order — and the caller supplies the words.
 */
export const EVENT_NAV_KEYS = [
  "details",
  "ticketTypes",
  "affiliateLinks",
  "sales",
  "holderList",
] as const;

export type EventNavKey = (typeof EVENT_NAV_KEYS)[number];

/**
 * One entry of the Event panel: where it points and what it is called, with the
 * calling being a key rather than a word.
 */
export type EventNavEntry = {
  key: EventNavKey;
  href: string;
  /** See `SidebarNavItem["exact"]`. */
  exact?: boolean;
};

/**
 * An Event's navigation, in the order it is read.
 *
 * The Event surface has a panel of its own that replaces the Organization's
 * while somebody works inside one Event, so these entries stand alone rather
 * than nesting under Events.
 *
 * org_admin and event_owner get full access; event_staff is limited. Affiliate
 * Links stays owner-only, but the Sales list is visible to every Member of the
 * Event — Event Staff included — so it appears for all roles. Tags have no
 * entry of their own: they are managed from within Details.
 *
 * Sales Trends has no entry either, since #460: it became a sub-tab of the
 * Sales surface (`salesNavItems`) rather than a sibling of it, so the same
 * chart is not offered from two places. The Sales entry lights while it is
 * being read, because Sales owns its subtree.
 *
 * The Holder List (#333; formerly the Outstanding Answers entry) follows that
 * rule twice over, which is why it takes a flag of its own rather than riding
 * `fullAccess`. Its route is gated to Org Admins alone — narrower than
 * `fullAccess`, which also admits an Event Owner — and the surface exists only
 * while EITHER Ticket Assignment or Ticket Questions is open, two features that
 * ship dark (ADR 0045). Both facts are the caller's to establish, because only
 * the caller holds the Event payload the flags arrive on and the Member's
 * actual role; this module keeps only the decision that the entry exists and
 * where it sits. Its href keeps the route's historical name — the screen is the
 * Holder List, the address is where it has always lived.
 */
export function eventNavItems({
  eventId,
  fullAccess,
  holderList = false,
}: {
  eventId: string;
  fullAccess: boolean;
  /**
   * Whether this reader may see the Event's Holder List: Ticket Assignment or
   * Ticket Questions is on AND they are an Org Admin. Defaults to false, so a
   * caller that has not thought about it gets the dark state the features ship
   * in rather than a tab that 404s.
   */
  holderList?: boolean;
}): EventNavEntry[] {
  return [
    // Details is the index of the Event, not its owner: without `exact` it would
    // stay lit on every page beneath the Event, so somebody reading the Sales
    // list would see two entries claiming to be the current page.
    { key: "details", href: `/events/${eventId}`, exact: true },
    { key: "ticketTypes", href: `/events/${eventId}/ticket-types` },
    ...(fullAccess
      ? [{ key: "affiliateLinks" as const, href: `/events/${eventId}/affiliate-links` }]
      : []),
    // No `exact`: Sales owns its subtree, so it stays lit on its own sub-tabs
    // (Record, Trends) — which is what a panel entry over a surface should do.
    { key: "sales", href: `/events/${eventId}/sales` },
    // The Holder List sits beside Sales because it is about who those sales
    // seat: every Ticket of the Event, who is coming on each, and — where the
    // Event asks questions — what each still owes.
    ...(holderList
      ? [{ key: "holderList" as const, href: `/events/${eventId}/outstanding-answers` }]
      : []),
  ];
}
