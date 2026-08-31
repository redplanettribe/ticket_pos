import type { AppLocale } from "@ticket-pos/locale";

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
 *
 * The link follows the reader's language, because the Terms are published in
 * both: sending an English reader to the Spanish page would be handing them the
 * one document they may not be able to read, right at the moment they are asked
 * to accept it. Which language they READ is not which text BINDS — the Spanish
 * prevails (§37) and the English page says so in its own first line — so the
 * page they land on carries that fact rather than this link having to.
 */
export function storefrontTermsUrl(locale: AppLocale): string {
  const base = (process.env.STOREFRONT_BASE_URL?.trim() || "http://localhost:64300").replace(
    /\/$/,
    "",
  );
  return `${base}/${locale}/terms`;
}
