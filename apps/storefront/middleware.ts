import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

import {
  AFFILIATE_REF_COOKIE,
  affiliateRefCookieOptions,
  rememberAffiliateClick,
} from "@/lib/affiliate-ref";
import { eventPageSlugs } from "@/lib/event-path";
import { LOCALE_COOKIE, localePrefixOf, localizedPath, resolveLocale } from "@/lib/locale";
import { isUnlocalized } from "@/lib/unlocalized-paths";

/**
 * The Storefront's one middleware, doing two unrelated jobs. Next runs exactly
 * one per app, so locale routing and the click half of Affiliate Attribution
 * (ADR 0022) share this file rather than each owning one — which is also why
 * there is a single matcher below and not one per feature.
 *
 * Locale routing: every page lives under a locale, and an address that names
 * none is sent to one.
 *
 *   1. A path that already names a locale is served as asked. It is not
 *      rewritten, nothing is sniffed to decide what it says, and no locale
 *      cookie is written. THE CONTENT OF A PREFIXED URL NEVER DEPENDS ON THE
 *      COOKIE OR ON ACCEPT-LANGUAGE — /en/... is English for everyone including
 *      crawlers, /es/... is Spanish for everyone, and a link one person sends
 *      another renders the same page at both ends. Anything added here that
 *      varies a prefixed response by who is asking breaks that, and breaks it
 *      silently.
 *   2. A path that names none is redirected into one, chosen by the precedence
 *      chain in lib/locale.ts. That chain picks a redirect TARGET and nothing
 *      else, which is the whole reason point 1 can hold.
 *
 * next-intl's own middleware is not used. It would do both of these plus locale
 * detection and a cookie write on prefixed requests, and the cookie here belongs
 * to the language switcher alone (see i18n/routing.ts).
 *
 * Affiliate Attribution: an Event page served with a `?ref=` remembers that code
 * for this Event, for the Attribution Window, and the begin-checkout route
 * carries it to the API when the buyer eventually pays. It lives in a middleware
 * because the cookie must be written on the response that serves the page, and a
 * Server Component cannot set one. Nothing about the page changes: the ref is
 * not validated, not resolved, and not shown — a live, mistyped, or long-dead
 * code all render the same Event, which is the promise the buyer's side of this
 * feature makes.
 *
 * The two meet on point 1, and the meeting is safe for a reason worth stating:
 * ATTACHING A SET-COOKIE IS NOT VARYING THE PAGE BY WHO IS ASKING. The rendered
 * Event page is byte-for-byte the same for the visitor with a ref, the visitor
 * without one and the crawler; only a cookie rides along beside it, read later
 * by the checkout route and by nothing that renders. The invariant is about
 * content, so it survives — and that is also why this branch sets no Vary, which
 * would be a claim about content that is not true here.
 */
export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  if (isUnlocalized(pathname)) {
    return NextResponse.next();
  }

  // Already in a language: served exactly as asked, with nothing read off the
  // request to decide what it says. This is the invariant above, enforced.
  if (localePrefixOf(pathname)) {
    const response = NextResponse.next();
    rememberRefFromEventPage(request, response);
    return response;
  }

  const locale = resolveLocale({
    cookie: request.cookies.get(LOCALE_COOKIE)?.value,
    acceptLanguage: request.headers.get("accept-language"),
  });

  const target = request.nextUrl.clone();
  // Path only: clone() brings the query string along, and it has to. An
  // Affiliate Link is written unprefixed — /{orgSlug}/events/{eventSlug}?ref=CODE
  // — so this redirect is the leg the ref has to survive. Drop it here and the
  // visitor lands on a page that renders perfectly and is attributed to nobody.
  // The cookie is then written on the prefixed request this redirect produces.
  target.pathname = localizedPath(locale, pathname);

  // Temporary, never permanent: the destination is decided per request, so a
  // 308 would let one visitor's browser cache a language for the next person on
  // the machine — and would be uncorrectable from the server afterwards. Vary
  // says the same thing to every cache in between: this answer was computed
  // from the request's cookie and its languages.
  const response = NextResponse.redirect(target, 307);
  response.headers.set("Vary", "Accept-Language, Cookie");
  return response;
}

/**
 * Remembers the Affiliate Link click this request is, if it is one, by writing
 * the ref cookie onto the response that serves the page.
 *
 * Every address the matcher admits passes through here, so the two questions —
 * is there a ref, and is this an Event page — are asked in that order and both
 * answered from the URL alone. An Affiliate Link points at one Event's page, so
 * a ref anywhere else names nothing and is left alone. That second question is
 * not a formality: the checkout success page spells its confirmation reference
 * `?ref=` too, and a middleware that only looked for the parameter would file
 * every completed purchase as a click on an Affiliate Link named after it.
 *
 * The slugs come from lib/event-path.ts rather than from an index into the path:
 * under a locale the Event page grew a segment, and code that counted segments
 * would go on running and quietly attribute nothing.
 */
function rememberRefFromEventPage(request: NextRequest, response: NextResponse): void {
  const ref = request.nextUrl.searchParams.get("ref");
  if (!ref) return;

  const slugs = eventPageSlugs(request.nextUrl.pathname);
  if (!slugs) return;

  const jar = rememberAffiliateClick(
    request.cookies.get(AFFILIATE_REF_COOKIE)?.value ?? null,
    slugs.orgSlug,
    slugs.eventSlug,
    ref,
    Date.now(),
  );
  // Null means there was nothing worth remembering — a ref that could not be a
  // code. The earlier click, if any, is deliberately left standing.
  if (jar !== null) {
    response.cookies.set(AFFILIATE_REF_COOKIE, jar, affiliateRefCookieOptions());
  }
}

export const config = {
  // One matcher for both jobs: the locale filter is a broad negative lookahead
  // that already admits an Event page in either shape, so the Affiliate half
  // needs nothing narrower of its own — and a second matcher would only be a
  // second place to forget an exclusion.
  //
  // It mirrors UNLOCALIZED_PREFIXES in lib/unlocalized-paths.ts, plus anything
  // with a dot in it, so the excluded routes are never woken by a middleware
  // invocation they would only decline. A matcher must be a literal — Next reads
  // it at build time and would ignore a computed value — so this cannot be
  // derived from the list. lib/unlocalized-paths.test.ts reads this very line
  // and fails when the two drift apart.
  matcher: ["/((?!api/|_next/|checkout/return|tickets/confirm|.*\\..*).*)"],
};
