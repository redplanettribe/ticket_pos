/**
 * Whether a sidebar navigation entry should be marked as the current page.
 *
 * An entry owns its whole subtree: a Platform Operator three levels into a
 * payout request still sees "Operator" lit up, rather than a sidebar insisting
 * they are nowhere. Matching is on path segment boundaries, so "/payouts" does
 * not claim "/payouts-archive" — a sibling route, not a descendant.
 *
 * The root entry is the exception: every path starts beneath "/", so it matches
 * exactly or it would claim every page in the app.
 */
export function isNavItemActive(activePath: string | undefined, href: string): boolean {
  if (!activePath) return false;
  if (href === "/") return activePath === "/";
  return activePath === href || activePath.startsWith(`${href}/`);
}
