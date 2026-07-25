/**
 * The Staff app's half of Google Sign-In: everything the browser touches, and
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
 * The client registered here is the STAFF one, distinct from the Storefront's.
 * That is what makes a code obtained on the Storefront unredeemable for a Staff
 * Session — Google binds a code to its issuing client, so the isolation is a
 * property of the credentials rather than of a check anywhere in this app.
 *
 * Everything here is a pure function over its arguments, apart from the two that
 * read randomness and the one that reads configuration. The route handlers hold
 * the Next-specific parts — reading cookies, writing them, redirecting — so the
 * rules worth getting wrong (state comparison, cookie shape, where a failure
 * lands) can be tested without a request.
 */

/** Google's OAuth 2.0 authorization endpoint, where the browser is sent. */
const AUTHORIZATION_ENDPOINT = "https://accounts.google.com/o/oauth2/v2/auth";

/**
 * The scope requested, and deliberately no more (PRD decision 4).
 *
 * `profile` would hand back `given_name` and `family_name`. Nothing on this
 * surface has anywhere to put them — a Member is an email and a role — and the
 * narrower scope shows a shorter consent screen. Nothing Google returns is
 * stored in any case.
 */
const SCOPE = "openid email";

/** Name of the short-lived cookie carrying one in-flight sign-in. */
export const GOOGLE_STATE_COOKIE = "ticket_pos_google_signin";

/**
 * The state cookie is scoped to the callback route and nowhere else, so it is
 * sent on exactly one request in the browser's life: the return from Google.
 * Ordinary use of the Staff app never carries it.
 */
export const GOOGLE_STATE_COOKIE_PATH = "/api/auth/google/callback";

/**
 * Ten minutes (PRD decision 8). It has to outlive an account picker, a consent
 * screen and possibly a Google password prompt, and it must not outlive the
 * browser tab it belongs to by long enough to matter. Nothing refreshes it: the
 * callback deletes it whatever the outcome, so the only cookie that ever expires
 * is one belonging to a sign-in that was abandoned.
 */
const STATE_COOKIE_MAX_AGE_SECONDS = 10 * 60;

/** Where the Google button points. */
export const GOOGLE_SIGN_IN_START_PATH = "/api/auth/google/start";

/**
 * Where every failed Google Sign-In lands: the login page, with one generic
 * message and a working passcode form (PRD decision 9).
 *
 * There is deliberately one message for all of them. A cancelled consent screen,
 * a `state` mismatch, an expired cookie and a refused exchange are
 * indistinguishable from out here, and they must stay that way: a path that
 * answered "we do not know that address" would hand back the enumeration oracle
 * the passcode request endpoint spends real effort denying.
 *
 * No destination travels with it. The Staff app has no post-sign-in `next` — the
 * auth-fork decides where a signed-in Member goes, and it decides it from the
 * session alone.
 */
export const GOOGLE_SIGN_IN_FAILURE_PATH = "/login?google=failed";

/**
 * This surface's public half of its Google registration.
 *
 * No secret appears here or anywhere else in this app. The client ID is public
 * information and the redirect URI is in every authorization URL, so both are
 * ordinary server-side environment variables. The environment names are the
 * generic ones because each app is mounted with its own surface's client; the
 * distinction lives in the deployment, not in the variable name.
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
 * without Google credentials, so the login page hides the button rather than
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
 * PendingSignIn is one sign-in in flight: the CSRF nonce to match on return and
 * the PKCE verifier to redeem the code with.
 *
 * Unlike the Storefront's, it carries no destination. Where a Member lands is
 * not something they chose before signing in — it is the auth-fork's answer,
 * computed from the session that the sign-in produces.
 */
export type PendingSignIn = {
  state: string;
  codeVerifier: string;
};

/**
 * newPendingSignIn mints the nonce and the PKCE verifier for one sign-in.
 *
 * Both are 32 bytes from the platform CSPRNG rendered as base64url: 43
 * characters, comfortably inside RFC 7636's 43-128 range for a code verifier and
 * far beyond guessing for a nonce.
 */
export function newPendingSignIn(): PendingSignIn {
  return { state: randomToken(), codeVerifier: randomToken() };
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
    // A Member signed into a personal Google account and a work one must be
    // asked which is theirs here, not silently signed in as whichever Google saw
    // last. Choosing wrong on this surface is the expensive mistake: an address
    // with no `members` row is offered organization CREATION, and a duplicate
    // Organization is what PRD decision 5's copy exists to prevent.
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
    new TextEncoder().encode(JSON.stringify({ s: pending.state, v: pending.codeVerifier })),
  );
}

/**
 * decodePendingSignIn reads the cookie back, returning null for anything that is
 * not a complete pending sign-in — absent, truncated, not base64url, not JSON,
 * missing a field.
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
  const { s, v } = parsed as { s?: unknown; v?: unknown };
  if (typeof s !== "string" || typeof v !== "string" || !s || !v) {
    return null;
  }
  return { state: s, codeVerifier: v };
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
