import type { SidebarNavItem } from "../components/sidebar-shell";

export type StaffNavVisibility = {
  /** Any member of an Organization. */
  showEvents?: boolean;
  /** Org Admins only: a Payout Request is theirs to make (CONTEXT.md). */
  showPayouts: boolean;
  /** Org Admins only. */
  showSettings?: boolean;
};

/**
 * The staff side panel's primary navigation, in the order it is read.
 *
 * The order is the point, and it is why this is a function rather than a list
 * with conditionals inlined at the render site: an entry that is hidden for most
 * roles still has one place it belongs, and Payouts belongs between POS and
 * Settings — after the daily work, before the housekeeping (#190).
 *
 * An omitted flag defaults to hidden: a caller that has not yet worked out
 * whether somebody may see an entry shows them less rather than more.
 *
 * `showPayouts` is the exception, and is required. It once fell back to
 * `showSettings` because both gates are `org_admin` today, which meant Payouts
 * would silently follow the wrong gate the day the two diverged (#191). An
 * entry guarding where an Organization's money is sent asks its caller to say.
 */
export function staffNavItems({
  showEvents = false,
  showPayouts = false,
  showSettings = false,
}: StaffNavVisibility): SidebarNavItem[] {
  return [
    { href: "/", label: "Dashboard" },
    ...(showEvents ? [{ href: "/events", label: "Events" }] : []),
    { href: "/pos", label: "POS" },
    ...(showPayouts ? [{ href: "/payouts", label: "Payouts" }] : []),
    ...(showSettings ? [{ href: "/settings", label: "Settings" }] : []),
  ];
}
