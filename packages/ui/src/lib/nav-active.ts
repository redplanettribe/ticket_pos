// The two questions a path is asked in this app, kept side by side because they
// are asked of the same path a line apart and answered by different rules:
// "which entry is current" (`isNavItemActive`) and "which surface am I on"
// (`isOnOperatorSurface`).

/** Where the Operator Dashboard begins; every page of it hangs beneath. */
const OPERATOR_SURFACE_ROOT = "/operator";

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
  return isPathWithin(activePath, href);
}

/**
 * Whether a path lies at or beneath a root, counting in whole path segments.
 *
 * The subtree rule on its own, without the question of what a sidebar should
 * light up: "/payouts" contains "/payouts/pr_123" but not "/payouts-archive",
 * which is a sibling route whose name merely starts the same way. Callers that
 * ask about a whole surface rather than about one entry (see
 * `isOnOperatorSurface`) want this and not `isNavItemActive`.
 */
function isPathWithin(activePath: string, root: string): boolean {
  return activePath === root || activePath.startsWith(`${root}/`);
}

/**
 * Whether the page being looked at belongs to the Operator Dashboard.
 *
 * A question about the surface, not about a navigation entry: it decides which
 * side panel the staff app wears at all (#192), and the surface owns its whole
 * subtree — an operator reading one Payout Request has not left it. The Overview
 * entry asks about the same "/operator" and is answered differently: as the index
 * of the surface it is current only AT "/operator" (`exact` in operator-nav.ts),
 * while the surface itself extends past it. Named apart so a reader does not have
 * to infer that difference from two bare calls.
 */
export function isOnOperatorSurface(activePath: string | undefined): boolean {
  if (!activePath) return false;
  return isPathWithin(activePath, OPERATOR_SURFACE_ROOT);
}
