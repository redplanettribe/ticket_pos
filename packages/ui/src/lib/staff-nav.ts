/**
 * The keys the Organization panel's entries are named by.
 *
 * A key and not a word, because the staff app is written in two languages (ADR
 * 0041) and this package cannot be handed a `t`: @ticket-pos/ui is shared with
 * the Storefront, whose catalog is deliberately a different one, and a module
 * that returned "Payouts" would be returning English to a Spanish reader with
 * nowhere to intercept it.
 *
 * So this module keeps the part that is a decision — which entries exist, for
 * whom, and in what order — and the caller supplies the words for the keys it
 * names. That is the same split `lib/login-copy.ts` makes in the staff app, and
 * it is why the tests below assert an order of keys rather than an order of
 * sentences: a copy edit is not a change to this module's behaviour.
 */
export const STAFF_NAV_KEYS = ["dashboard", "events", "payouts", "settings"] as const;

export type StaffNavKey = (typeof STAFF_NAV_KEYS)[number];

/**
 * One entry of the Organization panel: where it points and what it is called,
 * with the calling being a key rather than a word.
 *
 * `label` is deliberately absent. A shell turns this into a `SidebarNavItem` by
 * looking each key up in the labels it was handed, which makes a missing
 * translation a compile error at the shell rather than a blank line in a panel.
 */
export type StaffNavEntry = {
  key: StaffNavKey;
  href: string;
  /** See `SidebarNavItem["exact"]`. */
  exact?: boolean;
};

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
 * roles still has one place it belongs, and Payouts belongs between the daily
 * work and the housekeeping of Settings (#190).
 *
 * POS has no entry: the page is still a placeholder, so the panel does not
 * advertise it. Its route stays reachable for whoever is building it.
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
}: StaffNavVisibility): StaffNavEntry[] {
  return [
    { key: "dashboard", href: "/" },
    ...(showEvents ? [{ key: "events" as const, href: "/events" }] : []),
    ...(showPayouts ? [{ key: "payouts" as const, href: "/payouts" }] : []),
    ...(showSettings ? [{ key: "settings" as const, href: "/settings" }] : []),
  ];
}
