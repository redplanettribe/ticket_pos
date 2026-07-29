/**
 * What the Storefront will accept as an Affiliate Link code, in one place.
 *
 * Two paths read the same `?ref=` on the same page load: the click counter
 * reports it (affiliate-click.ts) and the attribution cookie remembers it
 * (affiliate-ref.ts). They must agree on the answer down to the character — two
 * rules would mean a ref that attributes a sale it never counted a click for,
 * which is exactly what a lowercase pasted ref used to do.
 */

/**
 * A code as an Affiliate Link issues it: unambiguous uppercase base32, eight
 * characters today. Matched loosely on length because the generator's width is
 * the backend's business, and strictly on alphabet because this value arrives
 * from a URL a stranger wrote — a ref is browser input like any other.
 *
 * Nothing here decides whether a code is LIVE. That is the API's verdict, and
 * asking it on a page view would mean an API call to answer a question whose
 * answer can change before the buyer pays (ADR 0022).
 */
export const AFFILIATE_CODE_PATTERN = /^[0-9A-Z]{4,32}$/;

/**
 * normalizeAffiliateCode turns a raw `?ref=` into the code the Affiliate Link
 * was issued under, or null when it could not be one.
 *
 * Uppercased, because the codes are drawn from an uppercase alphabet and a
 * buyer who retyped one in lower case followed a real link. A repeated ref
 * arrives from Next as an array; the first one is the link that was followed.
 */
export function normalizeAffiliateCode(
  raw: string | string[] | null | undefined,
): string | null {
  const value = Array.isArray(raw) ? raw[0] : raw;
  if (typeof value !== "string") return null;
  const code = value.trim().toUpperCase();
  return AFFILIATE_CODE_PATTERN.test(code) ? code : null;
}
