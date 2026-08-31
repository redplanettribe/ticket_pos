import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { callBackend } from "@/lib/api";
import { resolveAuthForkPath, type SessionForkInput } from "@/lib/auth-fork";
import {
  GOOGLE_SIGN_IN_FAILURE_PATH,
  GOOGLE_STATE_COOKIE,
  clearedGoogleStateCookieOptions,
  decodePendingSignIn,
  googleSignInConfig,
  statesMatch,
} from "@/lib/google-signin";
import { PENDING_TERMS_COOKIE, pendingTermsCookieOptions } from "@/lib/pending-terms";
import { detectedStaffLocale } from "@/lib/request-locale";
import { SESSION_COOKIE_NAME, sessionCookieOptions } from "@/lib/session";

// Reads one cookie and writes two. Never cached, never prerendered.
export const dynamic = "force-dynamic";

type VerifyData = {
  session_id: string;
  session: (SessionForkInput & { email: string }) | null;
  terms_required: { pending_terms_token: string } | null;
};

/**
 * Where Google returns the browser after the account picker.
 *
 * This handler holds the CSRF half of the flow and none of the identity half
 * (PRD decision 2). It proves that the response belongs to a sign-in this origin
 * started — by matching the returned `state` against the cookie — and then hands
 * the authorization code to the API, which owns the staff client secret and
 * decides whose email it is. It cannot name an address, so a bug here cannot
 * mint a session for one (ADR 0011).
 *
 * What happens after the session exists is the passcode path's own ending,
 * reached through the same `resolveAuthForkPath`: dashboard, organization
 * picker, or organization creation, with a lone membership auto-selected. The
 * sign-in method must never change where somebody lands, which is why this
 * computes nothing of its own here.
 *
 * Every failure returns the Member to /login with one generic message and a
 * usable passcode form. There is no error page and no diagnosis: a cancelled
 * consent screen, a forged `state`, an expired cookie and an exchange Google
 * refused all look identical from the outside, which is what keeps this path
 * from becoming the "is this address known?" oracle the passcode request
 * endpoint refuses to be (PRD decision 9).
 */
export async function GET(request: Request) {
  const params = new URL(request.url).searchParams;
  const store = await cookies();

  // Read then immediately schedule the deletion, before any branch below can
  // return: the cookie is spent the moment it is read, whatever happens next.
  // One in-flight sign-in, one use — a replayed callback finds nothing.
  const pending = decodePendingSignIn(store.get(GOOGLE_STATE_COOKIE)?.value);
  store.set(GOOGLE_STATE_COOKIE, "", clearedGoogleStateCookieOptions());

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
    return redirectTo(GOOGLE_SIGN_IN_FAILURE_PATH);
  }

  let result: VerifyData | null;
  try {
    const envelope = await callBackend<VerifyData>("/api/v1/auth/google/verify", {
      method: "POST",
      body: JSON.stringify({
        code,
        code_verifier: pending.codeVerifier,
        redirect_uri: config.redirectUri,
        // The same write the passcode path makes, on the sign-in method that
        // never touches the browser again: the API records this as the Staff
        // Locale only if the person has none (ADR 0041). Both doors must record
        // it or a Member's language would depend on which one they came through.
        locale: await detectedStaffLocale(),
      }),
    });
    result = envelope.data;
  } catch {
    // Includes the API refusing the exchange, Google refusing the code, an
    // unverified address, a missing `email` claim, and the API being
    // unreachable. All one message.
    return redirectTo(GOOGLE_SIGN_IN_FAILURE_PATH);
  }

  // Google proved the address, and the terms gate held the sign-in (#538): not
  // a failure, a step. The single-use token rides an httpOnly cookie to the
  // login page — a redirect has no page to hand it to — and the page shows the
  // terms card. The passcode form reaches the same card from its own verify
  // response; both doors are equal proof and both meet the same gate.
  if (result?.terms_required?.pending_terms_token) {
    store.set(
      PENDING_TERMS_COOKIE,
      result.terms_required.pending_terms_token,
      pendingTermsCookieOptions(),
    );
    return redirectTo("/login?terms=required");
  }

  if (!result?.session_id || !result.session) {
    return redirectTo(GOOGLE_SIGN_IN_FAILURE_PATH);
  }

  // The session token goes straight from this process into the same
  // `ticket_pos_session` cookie the passcode path writes, with the same
  // attributes and the same fourteen-day lifetime. It is never rendered, never
  // returned in a body, and never reaches a page script — this is a redirect, so
  // there is no body at all. Nothing about it records that Google was involved.
  store.set(SESSION_COOKIE_NAME, result.session_id, sessionCookieOptions());

  const fork = resolveAuthForkPath(result.session);

  // A Member of exactly one Organization has it selected for them, which is what
  // puts them on the dashboard rather than a picker with one row. The passcode
  // path does this from the browser (applyAuthFork); here there is no browser to
  // do it from yet, so the same call is made server-side with the session just
  // minted. Same call, same effect, same destination.
  if (fork.autoSelectMemberId) {
    try {
      await callBackend<unknown>("/api/v1/staff/session/organization", {
        method: "POST",
        body: JSON.stringify({ member_id: fork.autoSelectMemberId }),
        sessionToken: result.session_id,
      });
    } catch {
      // The sign-in itself succeeded, so this is not a failed sign-in and must
      // not be reported as one. They are signed in with nothing selected, which
      // is precisely what the picker is for.
      return redirectTo("/select-organization");
    }
  }

  return redirectTo(fork.path);
}

/**
 * Redirects within the Staff app using a relative Location: NextResponse.redirect
 * needs an absolute URL and would have to guess this app's public origin.
 */
function redirectTo(path: string): NextResponse {
  return new NextResponse(null, { status: 303, headers: { Location: path } });
}
