import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { safeNext } from "@/lib/destination";
import { safeFollowIntent } from "@/lib/follow-intent";
import {
  GOOGLE_STATE_COOKIE,
  authorizationUrl,
  codeChallengeS256,
  encodePendingSignIn,
  googleSignInConfig,
  googleStateCookieOptions,
  newPendingSignIn,
} from "@/lib/google-signin";
import { localizedPath } from "@/lib/locale";
import { redirectLocale } from "@/lib/redirect-locale";

// Mints a nonce and writes a cookie on every request. Nothing about it may be
// cached or prerendered.
export const dynamic = "force-dynamic";

/**
 * Where the Google button sends the browser.
 *
 * A GET route handler rather than a page because it is a navigation that has to
 * set a cookie, and only a route handler can do both — the same reason
 * /tickets/confirm is one. The visitor never sees it: it mints one pending
 * sign-in, remembers it in a cookie only the callback will ever be sent, and
 * hands the browser to Google.
 *
 * Nothing secret is involved. The client ID and redirect URI in the URL below
 * are public by design; the only thing worth protecting is the PKCE verifier,
 * and that stays in an httpOnly cookie on this origin while Google is told
 * nothing but its SHA-256.
 */
export async function GET(request: Request) {
  const config = googleSignInConfig();
  const params = new URL(request.url).searchParams;
  const destination = safeNext(params.get("next"));
  // The Follow the visitor pressed before signing in (#219). It is guarded by
  // newPendingSignIn on the way into the cookie and again on the way out, and it
  // never reaches Google — the cookie is what crosses the redirect.
  const followIntent = params.get("follow");

  // No credentials configured: the button that leads here is hidden, so this is
  // either a stale bookmark or a hand-typed URL. It is not an error worth a page
  // — send them to the passcode form, which works with no Google account at all.
  if (!config) {
    // A page, so it needs a language; this route has none of its own to pass on.
    const locale = await redirectLocale();
    // The intent goes back with them: the passcode form they are being handed is
    // a door that works, and it must not cost them what they pressed.
    const safe = safeFollowIntent(followIntent);
    const query = new URLSearchParams({ next: destination });
    if (safe) {
      query.set("follow", safe);
    }
    return redirectTo(localizedPath(locale, `/signin?${query.toString()}`));
  }

  const pending = newPendingSignIn(destination, followIntent);
  const codeChallenge = await codeChallengeS256(pending.codeVerifier);

  const store = await cookies();
  store.set(GOOGLE_STATE_COOKIE, encodePendingSignIn(pending), googleStateCookieOptions());

  return redirectTo(authorizationUrl(config, { state: pending.state, codeChallenge }));
}

/**
 * Redirects with a bare Location header rather than NextResponse.redirect, which
 * needs an absolute URL and would have to reconstruct this app's origin from the
 * incoming request — behind a proxy or a container bind address, that is the
 * wrong host. Google's URL is absolute and resolves regardless; a Storefront path
 * is resolved by the browser against the URL it actually asked for.
 */
function redirectTo(location: string): NextResponse {
  return new NextResponse(null, { status: 303, headers: { Location: location } });
}
