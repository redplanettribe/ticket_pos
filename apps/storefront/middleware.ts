import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

import { LOCALE_COOKIE, localePrefixOf, localizedPath, resolveLocale } from "@/lib/locale";

/**
 * Locale routing for the Storefront: every page lives under a locale, and an
 * address that names none is sent to one.
 *
 * This middleware does exactly two things, and deliberately not a third:
 *
 *   1. A path that already names a locale passes through untouched. It is not
 *      rewritten, nothing is sniffed, and no cookie is written. THE CONTENT OF
 *      A PREFIXED URL NEVER DEPENDS ON THE COOKIE OR ON ACCEPT-LANGUAGE —
 *      /en/... is English for everyone including crawlers, /es/... is Spanish
 *      for everyone, and a link one person sends another renders the same page
 *      at both ends. Anything added here that varies a prefixed response by who
 *      is asking breaks that, and breaks it silently.
 *   2. A path that names none is redirected into one, chosen by the precedence
 *      chain in lib/locale.ts. That chain picks a redirect TARGET and nothing
 *      else, which is the whole reason point 1 can hold.
 *
 * next-intl's own middleware is not used. It would do both of these plus
 * locale detection and a cookie write on prefixed requests, and the cookie here
 * belongs to the language switcher alone (see i18n/routing.ts).
 */

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
 * The matcher below repeats this list because a Next matcher must be a literal
 * pattern and cannot read a constant. The two must be changed together; this
 * one is the authority, and the matcher is the cheap filter that keeps most
 * requests from reaching this function at all.
 */
const UNLOCALIZED_PREFIXES = ["/api", "/_next", "/checkout/return", "/tickets/confirm"];

function isUnlocalized(pathname: string): boolean {
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

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  if (isUnlocalized(pathname)) {
    return NextResponse.next();
  }

  // Already in a language: served exactly as asked, with nothing read off the
  // request and no cookie written back. This is the invariant above, enforced.
  if (localePrefixOf(pathname)) {
    return NextResponse.next();
  }

  const locale = resolveLocale({
    cookie: request.cookies.get(LOCALE_COOKIE)?.value,
    acceptLanguage: request.headers.get("accept-language"),
  });

  const target = request.nextUrl.clone();
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

export const config = {
  // Mirrors UNLOCALIZED_PREFIXES above, plus anything with a dot in it. Kept in
  // the matcher as well as in code so the excluded routes are never woken by a
  // middleware invocation they would only decline.
  matcher: ["/((?!api/|_next/|checkout/return|tickets/confirm|.*\\..*).*)"],
};
