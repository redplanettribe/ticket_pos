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
  "reach",
  "sales",
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
 * org_admin and event_owner get full access; event_staff is limited. Reach —
 * how the Event's page was reached, and the Affiliate Links it is reached with
 * (#464) — stays owner-only, the gate Affiliate Links always carried, but the
 * Sales list is visible to every Member of the Event — Event Staff included —
 * so it appears for all roles. Tags have no entry of their own: they are
 * managed from within Details.
 *
 * Sales Trends has no entry either, since #460: it became a sub-tab of the
 * Sales surface (`salesNavItems`) rather than a sibling of it, so the same
 * chart is not offered from two places. The Sales entry lights while it is
 * being read, because Sales owns its subtree.
 *
 * The Holder List (#333) has no entry either, since #469: it is the last tab
 * of that same Sales surface, because the roster is a reading of the sales —
 * who they seat — exactly as the chart is. Its gate (Org Admin, and a feature
 * that is on) travels with it to `salesNavItems`.
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
    // No `exact`: Reach owns its subtree, so it stays lit on its own sub-tabs
    // (Trends, Affiliate Links). Affiliate Links has no entry of its own since
    // #464 — it is the second tab of this surface, not a sibling of it.
    ...(fullAccess ? [{ key: "reach" as const, href: `/events/${eventId}/reach` }] : []),
    // No `exact`: Sales owns its subtree, so it stays lit on its own sub-tabs
    // (Record, Trends, Holder List) — which is what a panel entry over a
    // surface should do.
    { key: "sales", href: `/events/${eventId}/sales` },
  ];
}
