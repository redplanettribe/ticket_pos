/**
 * The Storefront's public origin, as this Staff deployment knows it.
 *
 * The Staff app hosts no copy of the Términos y Condiciones (#538, ADR 0066):
 * its terms step links OUT to the public Storefront terms page, so what it
 * needs is an address, not a document. STOREFRONT_BASE_URL is the same runtime
 * variable the API reads for Confirmation Links, read per request rather than
 * baked at build for the reason lib/api.ts reads API_URL that way — one image,
 * any environment. Off-platform (local `next dev` without Compose) it falls to
 * the dev Storefront origin, so the link is followable on a developer's own
 * stack and never points at production.
 */
export function storefrontTermsUrl(): string {
  const base = (process.env.STOREFRONT_BASE_URL?.trim() || "http://localhost:64300").replace(
    /\/$/,
    "",
  );
  // The Spanish page: the terms are Spanish only and the Spanish text prevails
  // (§37) — every locale serves the same document, so the link names the one
  // whose words are the contract.
  return `${base}/es/terms`;
}
