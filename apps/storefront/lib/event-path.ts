/**
 * Reading an Event out of a Storefront address.
 *
 * An Event page is `/{orgSlug}/events/{eventSlug}`, and since the Storefront
 * gained languages it is served at `/{locale}/{orgSlug}/events/{eventSlug}` —
 * so any code that wants the two slugs has to cope with both shapes. The
 * middleware is the caller that matters: it holds a raw pathname before Next
 * has matched a route, has no `params` to read, and is where an Affiliate Link
 * click is remembered.
 *
 * Written as a match on the whole path rather than as an index into it. The
 * off-by-one this replaces was the expensive kind: an Affiliate Link whose
 * click stopped being attributed while the page it landed on kept rendering
 * perfectly, so nothing anywhere reported a fault.
 *
 * Pure and framework-free, so node:test can reach it — the ".ts" specifier is
 * written out for the same reason (see lib/locale.ts).
 */

import { isAppLocale } from "./locale.ts";

/** The two slugs that name an Event, in the order the URL states them. */
export type EventPageSlugs = {
  orgSlug: string;
  eventSlug: string;
};

/**
 * The Event an address names, or null when it names none.
 *
 * A leading Locale is optional and is stepped over rather than counted on, so
 * both the prefixed and the unprefixed shape answer the same. Everything else
 * — listings, the Customer Area, an Organization's own page, a deeper path
 * under an Event — is not an Event page and answers null.
 *
 * The slugs are returned exactly as the URL spelled them. Whether they name a
 * real Event is the API's question, not this function's: the middleware has to
 * decide what to remember before anything has been looked up.
 */
export function eventPageSlugs(pathname: string): EventPageSlugs | null {
  const segments = pathname.split("/").filter((segment) => segment !== "");
  // A trailing "/en" or "/es" of an Organization slug is not a Locale — only a
  // whole leading segment is, which is the same rule localePrefixOf applies.
  if (isAppLocale(segments[0])) segments.shift();

  const [orgSlug, events, eventSlug] = segments;
  if (segments.length !== 3 || events !== "events") return null;
  if (!orgSlug || !eventSlug) return null;

  return { orgSlug, eventSlug };
}
