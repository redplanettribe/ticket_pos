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
  "trends",
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
 * Links and Trends stay owner-only, but the Sales list is visible to every
 * Member of the Event — Event Staff included — so it appears for all roles.
 * Tags have no entry of their own: they are managed from within Details.
 *
 * Trends is hidden from Event Staff rather than shown and refused: the Sales
 * Trends surface carries the same guard the Event's money already has, and
 * offering a tab that answers 403 is worse than not offering it.
 */
export function eventNavItems({
  eventId,
  fullAccess,
}: {
  eventId: string;
  fullAccess: boolean;
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
    { key: "sales", href: `/events/${eventId}/sales` },
    // Trends reads the sales the tab above it lists, so it follows them.
    ...(fullAccess ? [{ key: "trends" as const, href: `/events/${eventId}/trends` }] : []),
  ];
}
