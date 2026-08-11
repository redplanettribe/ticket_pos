/**
 * The Storefront's half of Google Sign-In: everything the browser touches, and
 * nothing that decides who anybody is.
 *
 * Under ADR 0008 the Go API is not publicly invocable, so Google cannot redirect
 * a browser to it. The redirects therefore have to happen on this origin, which
 * splits the flow in two (PRD decision 2): this app mints the CSRF `state` nonce
 * and the PKCE verifier into a short-lived httpOnly cookie and bounces the
 * browser to Google, and the API — which holds the client secret — redeems the
 * authorization code and decides which email Google vouched for. This module
 * never names an email address and never sees one; it relays a code only Google
 * can turn into an identity (ADR 0011).
 *
 * Everything here is a pure function over its arguments, apart from the two that
 * read randomness and the one that reads configuration. The route handlers hold
 * the Next-specific parts — reading cookies, writing them, redirecting — so the
 * rules worth getting wrong (state comparison, cookie shape, where a failure
 * lands) can be tested without a request.
 */

// The ".ts" is written out because the unit tests run this module directly under
// `node --experimental-strip-types`, which resolves specifiers exactly. Next
// resolves it identically.
import { DEFAULT_DESTINATION, safeNext } from "./destination.ts";
import { safeFollowIntent } from "./follow-intent.ts";

/** Google's OAuth 2.0 authorization endpoint, where the browser is sent. */
const AUTHORIZATION_ENDPOINT = "https://accounts.google.com/o/oauth2/v2/auth";

/**
 * The scope requested, and deliberately no more.
 *
 * `profile` is here for exactly one claim: `picture`, which seeds a Customer
 * Avatar into an empty slot at sign-in. That is a deliberate widening of PRD
 * decision 4, which originally stopped at `openid email` — the Avatar earns the
 * slightly longer consent screen; nothing else the scope offers does.
 *
 * The names the scope also hands back (`given_name`, `family_name`) remain
 * refused: the API reads past them exactly as it reads past `sub`. Names have a
 * precedence rule of their own (prd-customer-login.md decisions 42-44) that a
 * third source would complicate, and nothing here needs a name to sign somebody
 * in.
 */
const SCOPE = "openid email profile";

/** Name of the short-lived cookie carrying one in-flight sign-in. */
export const GOOGLE_STATE_COOKIE = "ticket_pos_google_signin";

/**
 * The state cookie is scoped to the callback route and nowhere else, so it is
 * sent on exactly one request in the browser's life: the return from Google.
 * Ordinary Storefront browsing never carries it.
 */
export const GOOGLE_STATE_COOKIE_PATH = "/api/customer/auth/google/callback";

/**
 * Ten minutes (PRD decision 8). It has to outlive an account picker, a consent
 * screen and possibly a Google password prompt, and it must not outlive the
 * browser tab it belongs to by long enough to matter. Nothing refreshes it: the
 * callback deletes it whatever the outcome, so the only cookie that ever expires
 * is one belonging to a sign-in that was abandoned.
 */
const STATE_COOKIE_MAX_AGE_SECONDS = 10 * 60;

/** Where the Google button points. */
export const GOOGLE_SIGN_IN_START_PATH = "/api/customer/auth/google/start";

/**
 * One surface's public half of its Google registration (PRD decision 3: the
 * Storefront and Staff are separate clients, so a code obtained here cannot be
 * redeemed for a Staff Session).
 *
 * No secret appears here or anywhere else in this app. The client ID is public
 * information and the redirect URI is in every authorization URL, so both are
 * ordinary server-side environment variables.
 */
export type GoogleSignInConfig = {
  clientId: string;
  /** Must match Google's registration exactly, and is echoed to the API on exchange. */
  redirectUri: string;
};

/**
 * googleSignInConfig returns this deployment's Google registration, or null when
 * it has none.
 *
 * Null is a supported state, not a fault: `make dev` has to work for a developer
 * without Google credentials, so the sign-in page hides the button rather than
 * offering one that dead-ends (PRD "Local development"). Both routes check it
 * too, because a hidden button is not an access control.
 */
export function googleSignInConfig(): GoogleSignInConfig | null {
  const clientId = process.env.GOOGLE_CLIENT_ID?.trim();
  const redirectUri = process.env.GOOGLE_REDIRECT_URI?.trim();
  if (!clientId || !redirectUri) {
    return null;
  }
  return { clientId, redirectUri };
}

/** True when the Google button should be offered at all. */
export function isGoogleSignInConfigured(): boolean {
  return googleSignInConfig() !== null;
}

/**
 * Where the Google button points, carrying the destination the visitor came for
 * and any Follow they pressed on the way (#219).
 *
 * Both travel as query on this origin only. Neither reaches Google: the start
 * route puts them in the state cookie a few lines below, which is what crosses
 * the redirect, so Google's logs never hold either one.
 */
export function googleSignInStartPath(
  destination: string,
  followIntent?: string | null,
): string {
  const params = new URLSearchParams();
  const safe = safeNext(destination);
  if (safe !== DEFAULT_DESTINATION) {
    params.set("next", safe);
  }
  const intent = safeFollowIntent(followIntent);
  if (intent) {
    params.set("follow", intent);
  }
  const query = params.toString();
  return query ? `${GOOGLE_SIGN_IN_START_PATH}?${query}` : GOOGLE_SIGN_IN_START_PATH;
}

/**
 * PendingSignIn is one sign-in in flight: the CSRF nonce to match on return, the
 * PKCE verifier to redeem the code with, and where the visitor was going.
 *
 * The destination travels in here rather than in Google's `state` parameter
 * (PRD decision 8). It stays on our origin, out of Google's logs, and out of
 * reach of anyone who can edit a query string — and it is still run through
 * `safeNext` when it is read back, so even a forged cookie cannot redirect
 * anybody off this Storefront.
 */
export type PendingSignIn = {
  state: string;
  codeVerifier: string;
  destination: string;
  /**
   * The Follow the visitor pressed before signing in, or null (#219).
   *
   * It rides in the cookie for the same reasons the destination does — it stays
   * on this origin and out of Google's logs — and it is re-guarded on the way
   * out by `safeFollowIntent`, so even a forged cookie can only name a
   * well-formed subject. It could not name a subscriber in any case: whose
   * Follow this becomes is decided by the session the API mints from Google's
   * answer, and this app never sees an email at all on this path.
   */
  followIntent: string | null;
};

/**
 * newPendingSignIn mints the nonce and the PKCE verifier for one sign-in.
 *
 * Both are 32 bytes from the platform CSPRNG rendered as base64url: 43
 * characters, comfortably inside RFC 7636's 43-128 range for a code verifier and
 * far beyond guessing for a nonce.
 */
export function newPendingSignIn(
  destination: string,
  followIntent?: string | null,
): PendingSignIn {
  return {
    state: randomToken(),
    codeVerifier: randomToken(),
    destination: safeNext(destination),
    followIntent: safeFollowIntent(followIntent),
  };
}

/** 32 bytes of CSPRNG output as base64url. */
export function randomToken(byteLength = 32): string {
  const bytes = new Uint8Array(byteLength);
  crypto.getRandomValues(bytes);
  return base64UrlEncode(bytes);
}

/**
 * codeChallengeS256 is the only thing about the verifier that Google is told at
 * authorization time. S256 rather than `plain`: the challenge travels in a URL
 * that Google, any browser extension, and every log in between can read, and it
 * must not be redeemable on its own.
 */
export async function codeChallengeS256(codeVerifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(codeVerifier));
  return base64UrlEncode(new Uint8Array(digest));
}

/** The Google authorization URL to bounce the browser to. */
export function authorizationUrl(
  config: GoogleSignInConfig,
  pending: { state: string; codeChallenge: string },
): string {
  const params = new URLSearchParams({
    client_id: config.clientId,
    redirect_uri: config.redirectUri,
    response_type: "code",
    scope: SCOPE,
    state: pending.state,
    code_challenge: pending.codeChallenge,
    code_challenge_method: "S256",
    // Somebody signed into several Google accounts — a personal address and a
    // work one — must be asked which bought the tickets, not silently signed in
    // as whichever Google saw last. The wrong choice here mints a Customer on an
    // address with no Ticket Sales, which looks exactly like a bug.
    prompt: "select_account",
  });
  return `${AUTHORIZATION_ENDPOINT}?${params.toString()}`;
}

/** The cookie attributes for one in-flight sign-in (PRD decision 8). */
export function googleStateCookieOptions() {
  return {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    // Lax, not Strict: Google's callback is a cross-site top-level GET, which is
    // exactly the navigation Lax allows and Strict would strip the cookie from.
    sameSite: "lax" as const,
    path: GOOGLE_STATE_COOKIE_PATH,
    maxAge: STATE_COOKIE_MAX_AGE_SECONDS,
  };
}

/** The attributes that erase the state cookie. Path must match to delete it. */
export function clearedGoogleStateCookieOptions() {
  return { ...googleStateCookieOptions(), maxAge: 0 };
}

/** Serialises a pending sign-in for the cookie: JSON, base64url so it needs no quoting. */
export function encodePendingSignIn(pending: PendingSignIn): string {
  return base64UrlEncode(
    new TextEncoder().encode(
      JSON.stringify({
        s: pending.state,
        v: pending.codeVerifier,
        d: pending.destination,
        // Omitted rather than written as null when there is none, so the
        // ordinary sign-in's cookie is exactly the length it always was.
        ...(pending.followIntent ? { f: pending.followIntent } : {}),
      }),
    ),
  );
}

/**
 * decodePendingSignIn reads the cookie back, returning null for anything that is
 * not a complete pending sign-in — absent, truncated, not base64url, not JSON,
 * missing a field.
 *
 * The destination is passed through `safeNext` here rather than trusted as
 * written, so the relative-path guard protects the final redirect even in the
 * case this cookie was somehow forged.
 */
export function decodePendingSignIn(raw: string | null | undefined): PendingSignIn | null {
  if (!raw) {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder().decode(base64UrlDecode(raw)));
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) {
    return null;
  }
  const { s, v, d, f } = parsed as { s?: unknown; v?: unknown; d?: unknown; f?: unknown };
  if (typeof s !== "string" || typeof v !== "string" || !s || !v) {
    return null;
  }
  return {
    state: s,
    codeVerifier: v,
    destination: safeNext(typeof d === "string" ? d : null),
    // Guarded on the way out for the same reason the destination is: this value
    // is about to be relayed to the API, and a cookie is not a place to start
    // trusting text from.
    followIntent: safeFollowIntent(typeof f === "string" ? f : null),
  };
}

/**
 * statesMatch compares the nonce Google returned against the one in the cookie.
 *
 * The comparison is length-checked first and then constant-time over the whole
 * string, so a mismatch tells an attacker nothing about how far they got. Empty
 * values never match: a missing `state` is a mismatch, not a pass.
 */
export function statesMatch(expected: string, returned: string | null | undefined): boolean {
  if (!expected || !returned || expected.length !== returned.length) {
    return false;
  }
  let difference = 0;
  for (let i = 0; i < expected.length; i += 1) {
    difference |= expected.charCodeAt(i) ^ returned.charCodeAt(i);
  }
  return difference === 0;
}

/**
 * signInFailurePath is where every failed Google Sign-In lands: the sign-in page,
 * with one generic message and a working passcode form (PRD decision 9).
 *
 * There is deliberately one message for all of them. A cancelled consent screen,
 * a `state` mismatch, an expired cookie and a refused exchange are indistinguishable
 * from out here, and they must stay that way: a path that answered "we do not
 * know that address" would hand back the enumeration oracle the passcode request
 * endpoint spends real effort denying (prd-customer-login.md decision 10).
 *
 * The destination survives the failure so that the passcode form below still
 * returns the visitor to the Event page they started from — and so does the
 * Follow they pressed (#219). Somebody who dismissed the account picker and fell
 * back to a passcode is the same person who pressed Follow two minutes ago;
 * dropping the intent here would make the fallback quietly cost them the thing
 * they came to do.
 */
export function signInFailurePath(destination: string, followIntent?: string | null): string {
  const safe = safeNext(destination);
  const params = new URLSearchParams({ google: "failed" });
  if (safe !== DEFAULT_DESTINATION) {
    params.set("next", safe);
  }
  const intent = safeFollowIntent(followIntent);
  if (intent) {
    params.set("follow", intent);
  }
  return `/signin?${params.toString()}`;
}

/**
 * Where a Google Sign-In that was HELD FOR CONSENT lands: the same sign-in page,
 * with a marker saying the step is waiting (#252).
 *
 * It is the twin of signInFailurePath above and deliberately not a use of it,
 * because the two outcomes are opposite. A failure proved nothing and offers the
 * passcode form; this proved the address and offers the consent step — so a
 * visitor here must not be shown "we could not sign you in", which would be
 * false, and must not be sent back through a passcode they no longer need.
 *
 * `consent=pending` is a MARKER AND NOT A CREDENTIAL. It says only "look for a
 * held sign-in", and the thing worth holding travels in the httpOnly cookie
 * beside it (lib/pending-consent.ts). Anybody can type this address; without the
 * cookie it renders the ordinary email step, which is what somebody who did type
 * it deserves.
 *
 * The destination and the Follow intent survive for the same reasons they
 * survive a failure: the visitor still has somewhere to be going, and the Follow
 * they pressed two minutes ago must not become the price of having been asked
 * about consent. Both are re-guarded here, as everywhere.
 */
export function signInConsentPath(destination: string, followIntent?: string | null): string {
  const safe = safeNext(destination);
  const params = new URLSearchParams({ consent: "pending" });
  if (safe !== DEFAULT_DESTINATION) {
    params.set("next", safe);
  }
  const intent = safeFollowIntent(followIntent);
  if (intent) {
    params.set("follow", intent);
  }
  return `/signin?${params.toString()}`;
}

// --- base64url ------------------------------------------------------------
//
// btoa/atob rather than Buffer, matching lib/api.ts: both exist in every runtime
// this module can be bundled for, and Buffer does not.

function base64UrlEncode(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 1) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function base64UrlDecode(value: string): Uint8Array {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), "=");
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}
