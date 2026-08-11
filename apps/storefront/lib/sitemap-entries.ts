/**
 * What the Storefront offers a crawler: every indexable page, in every Locale,
 * each one annotated with its translations.
 *
 * This exists because /es is reachable almost only through the language
 * switcher. A crawler that never clicks it would find the Spanish Storefront
 * slowly or not at all, so the sitemap is where the Spanish half of the site is
 * declared rather than discovered.
 *
 * Two seams live here, both free of Next and of fetch, so the document a
 * crawler will be handed can be asserted in a unit test instead of read out of
 * a running server:
 *
 *   - collectSitemapEvents walks the cursor-paginated public listing through an
 *     injected fetcher, and stops.
 *   - sitemapEntries turns what the walk found into locale-annotated entries.
 *
 * The listing it walks returns only Discoverable, not-yet-ended Events. That is
 * not a limitation to work around: an Event nobody may discover and an Event
 * that already happened are both pages that should not be advertised to a
 * crawler, so the endpoint's own filter is the sitemap's filter.
 */

// The ".ts" is written out because the unit tests run this module directly
// under `node --experimental-strip-types`, which resolves specifiers exactly.
// Next resolves it identically.
import { localeAlternates } from "./alternates.ts";
import { LOCALES } from "./locale.ts";
import { PRIVACY_POLICY_PATH } from "./privacy-policy.ts";

/** One Event, reduced to the two slugs its address is built from. */
export type SitemapEvent = {
  organizationSlug: string;
  slug: string;
};

/**
 * One line of the sitemap: an address, plus the same page in every Locale.
 *
 * Structurally a `MetadataRoute.Sitemap` element, spelled out here so this
 * module stays runnable outside Next. `lastModified`, `changeFrequency` and
 * `priority` are deliberately absent — the public listing carries no
 * modification time, and the two hint fields are read by nobody. Emitting a
 * `lastModified` of "now" on every entry would be a claim that every page
 * changed every hour, which teaches a crawler to stop believing the field.
 */
export type SitemapEntry = {
  url: string;
  alternates: { languages: Record<string, string> };
};

/**
 * Page size for the walk: the largest the API will serve
 * (backend/internal/catalog/service/public_service.go, maxPublicEventLimit).
 * Asking for more is not an error there, it is silently clamped — so this
 * number is the real one, and raising it alone would only add round trips.
 */
export const SITEMAP_PAGE_SIZE = 50;

/**
 * The hard stop on the walk: at most this many pages, whatever the API says.
 *
 * A route with no cap is one data explosion away from walking forever while a
 * crawler holds the connection open. 100 pages of 50 is 5,000 Events, and the
 * worst case that produces — every Event under its own Organization — is
 * (1 root + 5,000 Organizations + 5,000 Events) x 2 Locales = 20,002 URLs,
 * comfortably inside the sitemap protocol's 50,000-URL limit. So the cap that
 * protects the route also keeps the document valid, and the two do not have to
 * be reasoned about separately.
 */
export const SITEMAP_MAX_PAGES = 100;

/** One page of the public Event listing, as the walk needs it. */
export type SitemapEventPage = {
  events: SitemapEvent[];
  nextCursor: string | null;
};

/**
 * Fetches one page. `null` means the call failed — the walk stops there and
 * keeps what it already has, because a partial sitemap is worth more to a
 * crawler than a 500.
 */
export type SitemapEventFetcher = (cursor?: string) => Promise<SitemapEventPage | null>;

export type SitemapWalk = {
  events: SitemapEvent[];
  pagesRead: number;
  /**
   * The cursor the walk refused to follow, or null when it reached the end.
   *
   * Non-null is the caller's cue to log. Truncating silently is the failure
   * mode that matters here: a short sitemap looks exactly like a small site,
   * so nothing about the output says the tail was dropped.
   */
  stoppedAtCursor: string | null;
};

/**
 * Walks the listing to its end, or to the cap, whichever comes first.
 *
 * The fetcher is injected so the walk's stopping conditions — cap reached, API
 * down, cursor that does not advance — can be exercised without a server.
 */
export async function collectSitemapEvents(
  fetchPage: SitemapEventFetcher,
  maxPages: number = SITEMAP_MAX_PAGES,
): Promise<SitemapWalk> {
  const events: SitemapEvent[] = [];
  const seen = new Set<string>();
  let cursor: string | undefined;
  let pagesRead = 0;

  while (pagesRead < maxPages) {
    const page = await fetchPage(cursor);
    if (!page) {
      // The API is down or unhappy. Stop with what we have rather than throw:
      // fetchData already degrades a failed read to nothing, and a sitemap
      // missing its tail still gets the rest of the site indexed.
      return { events, pagesRead, stoppedAtCursor: cursor ?? null };
    }
    pagesRead += 1;
    events.push(...page.events);

    const next = page.nextCursor;
    if (!next) {
      return { events, pagesRead, stoppedAtCursor: null };
    }
    // A cursor that repeats is a paginator that is not advancing, and following
    // it would loop until the cap with the same page over and over. Treat it as
    // the end: the caller learns the tail is missing either way.
    if (seen.has(next)) {
      return { events, pagesRead, stoppedAtCursor: next };
    }
    seen.add(next);
    cursor = next;
  }

  return { events, pagesRead, stoppedAtCursor: cursor ?? null };
}

/**
 * The locale-independent paths worth advertising, in the order a crawler meets
 * them: the explorer root, the Privacy Policy, then each Organization, then
 * each Event.
 *
 * Organizations are derived from the Events, because no endpoint lists them —
 * and deduped, because a listing page of one Organization's ten Events would
 * otherwise publish that Organization ten times. First appearance wins, which
 * keeps the soonest-first order of the listing.
 *
 * Nothing noindexed can arrive here. The Customer Area and the checkout
 * terminal pages are not built from Event data and have no way in; the only
 * paths this function can produce are "/", the Privacy Policy's, "/{org}" and
 * "/{org}/events/{event}".
 */
export function sitemapPaths(events: readonly SitemapEvent[]): string[] {
  const organizations: string[] = [];
  const seenOrganizations = new Set<string>();
  const eventPaths: string[] = [];
  const seenEvents = new Set<string>();

  for (const event of events) {
    if (!event.organizationSlug || !event.slug) continue;

    if (!seenOrganizations.has(event.organizationSlug)) {
      seenOrganizations.add(event.organizationSlug);
      organizations.push(`/${event.organizationSlug}`);
    }
    const path = `/${event.organizationSlug}/events/${event.slug}`;
    if (!seenEvents.has(path)) {
      seenEvents.add(path);
      eventPaths.push(path);
    }
  }

  // The Privacy Policy, declared in both languages like every other indexable
  // page (#250). It is the one path here that comes from no Event and no
  // Organization: a legal notice is published so that it can be found, and this
  // sitemap is the only way the Spanish half of this site is discovered at all.
  return ["/", PRIVACY_POLICY_PATH, ...organizations, ...eventPaths];
}

/**
 * The finished sitemap: every path above, once per Locale, each entry carrying
 * the full reciprocal hreflang map that page's own <head> carries.
 *
 * The addresses come from localeAlternates so the sitemap and the pages cannot
 * disagree — a canonical in the HTML that differs from the loc in the sitemap
 * is a contradiction a crawler resolves by trusting neither.
 *
 * Off-platform there is no origin (lib/site.ts), and a sitemap of relative
 * addresses is not a lesser sitemap, it is an invalid one: Next writes the
 * `url` into <loc> verbatim, with no metadataBase to resolve it against, and a
 * relative <loc> is rejected. So without an origin this publishes nothing,
 * which is the truth about a preview stack no crawler was ever going to read.
 */
export function sitemapEntries(events: readonly SitemapEvent[], base?: URL): SitemapEntry[] {
  if (!base) return [];

  return sitemapPaths(events).flatMap((path) =>
    LOCALES.map((locale) => {
      const { canonical, languages } = localeAlternates(path, locale, base);
      return { url: canonical, alternates: { languages } };
    }),
  );
}
