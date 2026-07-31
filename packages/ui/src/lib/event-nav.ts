/**
 * An Event's navigation, in the order it is read.
 *
 * The Event surface has a panel of its own that replaces the Organization's
 * while somebody works inside one Event, so these entries stand alone rather
 * than nesting under Events.
 *
 * org_admin and event_owner get full access; event_staff is limited. Tags and
 * Affiliate Links stay owner-only, but the Sales list is visible to every
 * Member of the Event — Event Staff included — so it appears for all roles.
 */
export type EventNavItem = {
  href: string;
  label: string;
  /** True for an entry that must not claim the pages beneath it. */
  exact?: boolean;
};

export function eventNavItems({
  eventId,
  fullAccess,
}: {
  eventId: string;
  fullAccess: boolean;
}): EventNavItem[] {
  return [
    // Details is the index of the Event, not its owner: without `exact` it would
    // stay lit on every page beneath the Event, so somebody reading the Sales
    // list would see two entries claiming to be the current page.
    { href: `/events/${eventId}`, label: "Details", exact: true },
    { href: `/events/${eventId}/ticket-types`, label: "Ticket Types" },
    ...(fullAccess
      ? [
          { href: `/events/${eventId}/tags`, label: "Tags" },
          { href: `/events/${eventId}/affiliate-links`, label: "Affiliate Links" },
        ]
      : []),
    { href: `/events/${eventId}/sales`, label: "Sales" },
  ];
}
