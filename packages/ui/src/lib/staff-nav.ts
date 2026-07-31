/**
 * The staff side panel's primary navigation, in the order it is read.
 *
 * The order is the point, and it is why this is a function rather than a list
 * with conditionals inlined at the render site: an entry that is hidden for most
 * roles still has one place it belongs, and Payouts belongs between POS and
 * Settings — after the daily work, before the housekeeping (#190).
 *
 * Every flag defaults to hidden. A caller that has not yet worked out whether
 * somebody may see an entry shows them less rather than more.
 */
export type StaffNavItem = { href: string; label: string };

export type StaffNavVisibility = {
  /** Any member of an Organization. */
  showEvents?: boolean;
  /** Org Admins only: a Payout Request is theirs to make (CONTEXT.md). */
  showPayouts?: boolean;
  /** Org Admins only. */
  showSettings?: boolean;
};

export function staffNavItems({
  showEvents = false,
  showPayouts = false,
  showSettings = false,
}: StaffNavVisibility): StaffNavItem[] {
  return [
    { href: "/", label: "Dashboard" },
    ...(showEvents ? [{ href: "/events", label: "Events" }] : []),
    { href: "/pos", label: "POS" },
    ...(showPayouts ? [{ href: "/payouts", label: "Payouts" }] : []),
    ...(showSettings ? [{ href: "/settings", label: "Settings" }] : []),
  ];
}
