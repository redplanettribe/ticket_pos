/**
 * Whether a sidebar navigation entry should be marked as the current page.
 *
 * An entry owns its whole subtree: a Platform Operator three levels into a
 * payout request still sees "Operator" lit up, rather than a sidebar insisting
 * they are nowhere. Matching is on path segment boundaries, so "/payouts" does
 * not claim "/payouts-archive" — a sibling route, not a descendant.
 *
 * The root entry is the exception: every path starts beneath "/", so it matches
 * exactly or it would claim every page in the app. `exact` asks for that same
 * treatment for an entry that is the index of its own surface — Overview at
 * "/operator" sits beside Payout Requests at "/operator/payout-requests", and
 * owning the subtree would leave it lit on every page of the Operator Dashboard
 * (#192).
 */
export function isNavItemActive(
  activePath: string | undefined,
  href: string,
  { exact = false }: { exact?: boolean } = {},
): boolean {
  if (!activePath) return false;
  if (exact || href === "/") return activePath === href;
  return activePath === href || activePath.startsWith(`${href}/`);
}
