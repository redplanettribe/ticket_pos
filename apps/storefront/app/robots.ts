import type { MetadataRoute } from "next";

import { LOCALES } from "@/lib/locale";
import { storefrontBaseUrl } from "@/lib/site";

/**
 * /robots.txt — what a crawler may walk, and where the map is.
 *
 * Unprefixed, and left alone by middleware.ts: the matcher excludes anything
 * containing a dot, so /robots.txt is never redirected into a Locale. A
 * crawler asks for it at the origin root and nowhere else.
 *
 * Rendered per request rather than baked at build. STOREFRONT_BASE_URL is a
 * runtime variable — one image serves every environment (lib/site.ts) — so a
 * prerendered robots.txt would be frozen with the origin its BUILD saw, which
 * is no origin at all, and a static route is never regenerated to correct it.
 * The file is three lines of text; generating it per request costs nothing.
 */
export const dynamic = "force-dynamic";

/**
 * Paths under a Locale that a crawler is asked to leave alone. Prefix matches,
 * so "/en/tickets" covers the whole Customer Area beneath it.
 *
 * These are the surfaces already carrying `robots: { index: false }` in their
 * metadata (app/[locale]/tickets, app/[locale]/checkout/*). Both statements are
 * needed and they do different jobs: noindex keeps a page out of results but
 * only after it has been crawled, and these are per-visitor pages behind a
 * Customer Session or a live checkout — there is nothing here worth spending a
 * crawl on in the first place.
 */
const LOCALIZED_DISALLOW = ["/tickets", "/checkout"];

/**
 * Unprefixed paths to keep out. These are not pages: /checkout/return is the
 * Payment Provider's return leg and /tickets/confirm is a Confirmation Link
 * from an email, both carrying single-use parameters, and /api is this app's
 * BFF. A crawler following one of them would be firing state changes at it.
 */
const UNLOCALIZED_DISALLOW = ["/api/", "/checkout/", "/tickets/"];

export default function robots(): MetadataRoute.Robots {
  const base = storefrontBaseUrl();

  return {
    rules: {
      userAgent: "*",
      allow: "/",
      disallow: [
        ...LOCALES.flatMap((locale) => LOCALIZED_DISALLOW.map((path) => `/${locale}${path}`)),
        ...UNLOCALIZED_DISALLOW,
      ],
    },
    // A Sitemap directive must be an absolute URL, so off-platform the line is
    // left out rather than emitted relative. Everything above still applies:
    // robots.txt without a sitemap is an ordinary robots.txt.
    ...(base ? { sitemap: new URL(`${base.pathname.replace(/\/+$/, "")}/sitemap.xml`, base).toString() } : {}),
  };
}
