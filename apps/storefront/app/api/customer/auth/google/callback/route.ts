import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import {
  CUSTOMER_SESSION_COOKIE,
  customerSessionCookieOptions,
  type CustomerVerifyResult,
} from "@/lib/customer-session";
import { DEFAULT_DESTINATION } from "@/lib/destination";
import {
  GOOGLE_STATE_COOKIE,
  clearedGoogleStateCookieOptions,
  decodePendingSignIn,
  googleSignInConfig,
  signInFailurePath,
  statesMatch,
} from "@/lib/google-signin";
import { localizedPath, type AppLocale } from "@/lib/locale";
import { redirectLocale } from "@/lib/redirect-locale";

// Reads one cookie and writes two. Never cached, never prerendered.
export const dynamic = "force-dynamic";

/**
 * Where Google returns the browser after the account picker.
 *
 * This handler holds the CSRF half of the flow and none of the identity half
 * (PRD decision 2). It proves that the response belongs to a sign-in this origin
 * started — by matching the returned `state` against the cookie — and then hands
 * the authorization code to the API, which owns the client secret and decides
 * whose email it is. It cannot name an address, so a bug here cannot mint a
 * session for one (ADR 0011).
 *
 * Every failure returns the visitor to /signin with one generic message and a
 * usable passcode form. There is no error page and no diagnosis: a cancelled
 * consent screen, a forged `state`, an expired cookie and an exchange Google
 * refused all look identical from the outside, which is what keeps this path
 * from becoming the "is this address known?" oracle the passcode request
 * endpoint refuses to be (PRD decision 9).
 */
export async function GET(request: Request) {
  const params = new URL(request.url).searchParams;
  const store = await cookies();
  // Google returns the browser to a redirect URI registered as a constant, so
  // the language of the page this hands off to is chosen here. The destination
  // carried through the state cookie is a locale-free path for the same reason.
  const locale = await redirectLocale();
  const redirectTo = (path: string) => localizedRedirect(locale, path);

  // Read then immediately schedule the deletion, before any branch below can
  // return: the cookie is spent the moment it is read, whatever happens next.
  // One in-flight sign-in, one use — a replayed callback finds nothing.
  const pending = decodePendingSignIn(store.get(GOOGLE_STATE_COOKIE)?.value);
  store.set(GOOGLE_STATE_COOKIE, "", clearedGoogleStateCookieOptions());

  // No cookie, no destination: an abandoned sign-in resumed after ten minutes,
  // or a callback nobody here started. Either way the Customer Area is the right
  // place to be heading.
  const destination = pending?.destination ?? DEFAULT_DESTINATION;

  const code = params.get("code");
  const config = googleSignInConfig();
  if (
    // The consent screen was dismissed: Google says error=access_denied.
    params.get("error") ||
    !pending ||
    !code ||
    !statesMatch(pending.state, params.get("state")) ||
    !config
  ) {
    return redirectTo(signInFailurePath(destination));
  }

  try {
    // Deliberately no sessionToken. A Customer who arrived on a Confirmation
    // Link holds a session scoped to one Ticket Sale, and signing in with Google
    // is them asking to widen it — so this call carries no existing session and
    // the API mints a full one, which then replaces the narrow cookie below.
    // (/tickets/confirm passes the session on purpose, for the opposite reason:
    // a link must never downgrade somebody already signed in.)
    const envelope = await callBackend<CustomerVerifyResult>(
      "/api/v1/customer/auth/google/verify",
      {
        method: "POST",
        body: JSON.stringify({
          code,
          code_verifier: pending.codeVerifier,
          redirect_uri: config.redirectUri,
        }),
      },
    );

    const result = envelope.data;
    if (!result?.session_id) {
      return redirectTo(signInFailurePath(destination));
    }

    // The session token goes straight from this process into an httpOnly cookie.
    // It is never rendered, never returned in a body, and never reaches a page
    // script — this is a redirect, so there is no body at all.
    store.set(CUSTOMER_SESSION_COOKIE, result.session_id, customerSessionCookieOptions());

    return redirectTo(destination);
  } catch {
    // Includes the API refusing the exchange, Google refusing the code, an
    // unverified address, and the API being unreachable. All one message.
    return redirectTo(signInFailurePath(destination));
  }
}

/**
 * Redirects into a localized page within this Storefront using a relative
 * Location, for the same reason /tickets/confirm does: NextResponse.redirect
 * needs an absolute URL and would have to guess this app's public origin.
 */
function localizedRedirect(locale: AppLocale, path: string): NextResponse {
  return new NextResponse(null, { status: 303, headers: { Location: localizedPath(locale, path) } });
}
