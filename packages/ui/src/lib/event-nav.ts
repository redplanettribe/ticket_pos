import type { SidebarNavItem } from "../components/sidebar-shell";

/**
 * An Event's navigation, in the order it is read.
 *
 * The Event surface has a panel of its own that replaces the Organization's
 * while somebody works inside one Event, so these entries stand alone rather
 * than nesting under Events.
 *
 * org_admin and event_owner get full access; event_staff is limited. Affiliate
 * Links stay owner-only, but the Sales list is visible to every Member of the
 * Event — Event Staff included — so it appears for all roles. Tags have no
 * entry of their own: they are managed from within Details.
 */
export function eventNavItems({
  eventId,
  fullAccess,
}: {
  eventId: string;
  fullAccess: boolean;
}): SidebarNavItem[] {
  return [
    // Details is the index of the Event, not its owner: without `exact` it would
    // stay lit on every page beneath the Event, so somebody reading the Sales
    // list would see two entries claiming to be the current page.
    { href: `/events/${eventId}`, label: "Details", exact: true },
    { href: `/events/${eventId}/ticket-types`, label: "Ticket Types" },
    ...(fullAccess
      ? [{ href: `/events/${eventId}/affiliate-links`, label: "Affiliate Links" }]
      : []),
    { href: `/events/${eventId}/sales`, label: "Sales" },
  ];
}
