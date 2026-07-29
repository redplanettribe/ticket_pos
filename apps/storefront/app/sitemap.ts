import type { MetadataRoute } from "next";

import { listPublicEvents } from "@/lib/api";
import { storefrontBaseUrl } from "@/lib/site";
import {
  SITEMAP_PAGE_SIZE,
  collectSitemapEvents,
  sitemapEntries,
} from "@/lib/sitemap-entries";

/**
 * /sitemap.xml — every indexable Storefront page, in both Locales.
 *
 * Unprefixed by design, and it must stay that way: a crawler asks for
 * /sitemap.xml, not /en/sitemap.xml, and the file describes the whole site
 * rather than one language of it. middleware.ts already lets it through — the
 * matcher excludes anything containing a dot — so no redirect stands between a
 * crawler and this route.
 *
 * Rendered per request, for the same reason robots.ts is: STOREFRONT_BASE_URL is
 * a runtime variable (lib/site.ts) and the Docker build never sees one, so a
 * route Next may prerender is a route whose origin at build time is `undefined`
 * — and this one answers that with an empty document. The image would then ship
 * a valid, well-formed, permanently empty sitemap, which is the failure the
 * whole ticket exists to avoid and which nothing about the output would reveal.
 *
 * The hourly reuse lives on the fetch instead of on the route, where it was
 * already doing the real work: `export const revalidate` alone was never what
 * made the walk cheap, because an uncached fetch inside the render pins the
 * route to request time anyway. The `{ revalidate }` below is what a second
 * crawler hit actually reads (lib/api.ts, ReadCache), and an explicit
 * `next.revalidate` on a fetch outranks the no-store default `force-dynamic`
 * would otherwise impose — so the 100-page walk still happens at most once an
 * hour, while the document that walk produces is built against the origin this
 * process is really running under.
 */
export const dynamic = "force-dynamic";

/** How long the Event walk may be reused; see the note above. */
const revalidate = 3600;

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const base = storefrontBaseUrl();
  // Off-platform there is no origin, and a <loc> is written verbatim with no
  // metadataBase behind it — so the choice is an empty sitemap or an invalid
  // one. Also skips the walk entirely on preview stacks, which is the right
  // amount of work to do for a document nothing will fetch.
  if (!base) {
    return [];
  }

  const walk = await collectSitemapEvents(async (cursor) => {
    const page = await listPublicEvents(
      { cursor, limit: SITEMAP_PAGE_SIZE },
      { revalidate },
    );
    if (!page) return null;
    return {
      // Organization URLs are derived from this: nothing lists Organizations,
      // and an Organization with no Discoverable Event has no page worth
      // advertising anyway.
      events: page.events.map((event) => ({
        organizationSlug: event.organization.slug,
        slug: event.slug,
      })),
      nextCursor: page.next_cursor,
    };
  });

  if (walk.stoppedAtCursor) {
    // Truncation is invisible in the output — a short sitemap reads as a small
    // site — so it is said out loud where an operator can find it.
    console.warn(
      `[sitemap] stopped after ${walk.pagesRead} page(s) and ${walk.events.length} Event(s); ` +
        `Events past cursor ${walk.stoppedAtCursor} are not listed`,
    );
  }

  return sitemapEntries(walk.events, base);
}
