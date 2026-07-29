/**
 * Addresses that are not pages and must never gain a locale.
 *
 * - /api/**: this app's BFF routes, called by its own scripts with fixed paths.
 * - /checkout/return: the Payment Provider builds this URL from a constant it
 *   was given at Payment time (backend/internal/sales/service/checkout.go,
 *   checkoutReturnPath). A redirect here would still work, but the provider's
 *   return leg is the last place to spend a round trip, and some providers
 *   POST it — a redirect would have to preserve the method to be safe.
 * - /tickets/confirm: a Confirmation Link lives in an email for the life of an
 *   Event; its address cannot acquire a language it did not have when it was
 *   sent.
 *
 * The same fact is stated a second time in middleware.ts's `config.matcher`,
 * because a Next matcher must be a literal pattern and cannot read a constant.
 * That copy is not kept honest by hand: unlocalized-paths.test.ts reads the
 * matcher out of middleware.ts and fails when a prefix added here is not
 * excluded there. This list is the authority; the matcher is the cheap filter
 * that keeps most requests from waking the middleware at all.
 *
 * The list lives here rather than beside the matcher precisely so the test can
 * import it — middleware.ts cannot be loaded by the unit tests, which resolve
 * specifiers exactly and know nothing of the "@/" alias.
 */
export const UNLOCALIZED_PREFIXES = ["/api", "/_next", "/checkout/return", "/tickets/confirm"];

export function isUnlocalized(pathname: string): boolean {
  const named = UNLOCALIZED_PREFIXES.some(
    (prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`),
  );
  if (named) {
    return true;
  }
  // Anything carrying a file extension is an asset or a metadata route —
  // /favicon.ico, /icon.svg, /apple-icon.png, /manifest.webmanifest. No page
  // path can contain a dot: Organization and Event slugs are [a-z0-9-] (see
  // app/api/checkout/route.ts), and the locale segment is two letters.
  return pathname.includes(".");
}
