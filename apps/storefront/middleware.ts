import { NextResponse, type NextRequest } from "next/server";

import {
  AFFILIATE_REF_COOKIE,
  affiliateRefCookieOptions,
  rememberAffiliateClick,
} from "@/lib/affiliate-ref";

/**
 * The click half of Affiliate Attribution (ADR 0022): an Event page served with
 * a `?ref=` remembers that code for this Event, for the Attribution Window, and
 * the begin-checkout route carries it to the API when the buyer eventually pays.
 *
 * It lives in middleware because the cookie must be written on the response that
 * serves the page, and a Server Component cannot set one. Nothing about the page
 * changes: the ref is not validated, not resolved, and not shown — a live,
 * mistyped, or long-dead code all render the same Event, which is the promise
 * the buyer's side of this feature makes.
 */
export function middleware(request: NextRequest) {
  const response = NextResponse.next();

  const ref = request.nextUrl.searchParams.get("ref");
  if (!ref) return response;

  // The matcher guarantees the shape; the slugs are simply the two path
  // segments that name the Event this click was for.
  const [, orgSlug, , eventSlug] = request.nextUrl.pathname.split("/");
  if (!orgSlug || !eventSlug) return response;

  const jar = rememberAffiliateClick(
    request.cookies.get(AFFILIATE_REF_COOKIE)?.value ?? null,
    orgSlug,
    eventSlug,
    ref,
    Date.now(),
  );
  // Null means there was nothing worth remembering — a ref that could not be a
  // code. The earlier click, if any, is deliberately left standing.
  if (jar !== null) {
    response.cookies.set(AFFILIATE_REF_COOKIE, jar, affiliateRefCookieOptions());
  }
  return response;
}

/**
 * Event pages only. An Affiliate Link points at one Event's page, so that is the
 * only URL a click can arrive on, and every other route — listings, checkout,
 * the Customer Area, the BFF — is left untouched by this middleware.
 */
export const config = {
  matcher: "/:orgSlug/events/:eventSlug",
};
