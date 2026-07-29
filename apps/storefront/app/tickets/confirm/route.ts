import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { APIError, callBackend } from "@/lib/api";
import {
  CUSTOMER_SESSION_COOKIE,
  confirmationLinkSessionCookieOptions,
  customerSessionCookieOptions,
  customerSessionToken,
  type CustomerVerifyResult,
} from "@/lib/customer-session";
import { localizedPath, type AppLocale } from "@/lib/locale";
import { redirectLocale } from "@/lib/redirect-locale";

// Redeeming touches a cookie and must never be cached or prerendered.
export const dynamic = "force-dynamic";

/**
 * Where a Confirmation Link lands.
 *
 * This is a GET route handler rather than a page because it is a navigation that
 * has to set a cookie, and only a route handler can do both. The Customer never
 * sees it: it redeems the token, takes custody of the session, and forwards them
 * to their tickets.
 *
 * The browser does not call the Go API here — this handler does, server-side,
 * exactly like every other route under /api/customer (ADR 0008). The token
 * arrives in the URL because it came out of an email; it goes no further than
 * this process, and what comes back is written straight into an httpOnly cookie.
 */
export async function GET(request: Request) {
  // A Confirmation Link is a fixed address that has been sitting in an inbox
  // since the sale, so it carries no locale of its own and this handler has to
  // choose one for the page it hands the Customer on to.
  //
  // Unlike the Payment Provider's return leg, there is no checkout context to
  // read it from and deliberately no attempt to find one: a link is opened days
  // later, from a mail client, often on another device entirely, so the browser
  // in front of us is the only evidence there is. A context cookie surviving
  // from some unrelated purchase in this jar would be worse evidence, not
  // better.
  const locale = await redirectLocale();
  const redirectTo = (path: string) => localizedRedirect(locale, path);

  const token = new URL(request.url).searchParams.get("token");
  if (!token) {
    return redirectTo("/signin?link=invalid");
  }

  // Whatever session this visitor already holds travels with the redemption. The
  // API returns it unchanged if it is a full Customer Session: a link must never
  // downgrade someone who is already signed in, and deciding that on the server
  // keeps the rule in one place rather than in every client that redeems.
  const existing = await customerSessionToken();

  try {
    const envelope = await callBackend<CustomerVerifyResult>("/api/v1/customer/auth/confirmation-link", {
      method: "POST",
      body: JSON.stringify({ token }),
      sessionToken: existing,
    });

    const result = envelope.data;
    if (!result?.session_id) {
      return redirectTo("/signin?link=invalid");
    }

    const store = await cookies();
    // A sale-scoped session lives about a day, so its cookie does too; a full
    // session that survived the redemption keeps its own long window.
    store.set(
      CUSTOMER_SESSION_COOKIE,
      result.session_id,
      result.session.ticket_sale_id
        ? confirmationLinkSessionCookieOptions()
        : customerSessionCookieOptions(),
    );

    return redirectTo("/tickets");
  } catch (error) {
    // A link that has run out and one that was never valid get different copy on
    // the sign-in page, because the recovery reads differently: the first person
    // definitely bought a ticket.
    const expired = error instanceof APIError && error.code === "CONFIRMATION_LINK_EXPIRED";
    return redirectTo(expired ? "/signin?link=expired" : "/signin?link=invalid");
  }
}

/**
 * Redirects into a localized page within this Storefront, using a relative
 * Location.
 *
 * Deliberately not NextResponse.redirect, which needs an absolute URL and would
 * have to reconstruct the origin from the incoming request — behind a proxy or
 * a container bind address that is the wrong host, and the Customer would be sent
 * somewhere unreachable. A relative Location is resolved by the browser against
 * the URL it actually asked for, which is by definition the right one.
 *
 * The path is written locale-free at every call site and gains the prefix here,
 * so this handler reads the same as the pages do.
 */
function localizedRedirect(locale: AppLocale, path: string): NextResponse {
  return new NextResponse(null, { status: 303, headers: { Location: localizedPath(locale, path) } });
}
