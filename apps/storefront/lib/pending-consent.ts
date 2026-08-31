/**
 * How a held sign-in survives Google's redirect (#252).
 *
 * The passcode door never has this problem. Its verify is a fetch made by a
 * script that is already on the sign-in page, so a consent-required outcome
 * comes back into the very component that has to render it and nothing has to
 * be carried anywhere. The Google door is a NAVIGATION: the browser leaves this
 * origin, comes back to a fixed callback address, and whatever the API answered
 * there has to reach a page that has not been rendered yet.
 *
 * So it travels the way everything else does across that redirect — the
 * destination, the PKCE verifier, the Follow intent — in a short-lived httpOnly
 * cookie on this origin. Not in the query string: the pending-consent token is
 * a credential, and a credential in an address ends up in browser history, in
 * this app's own request logs, and in the `Referer` of the very first link the
 * consent step offers (the Privacy Policy, which opens in a new tab). It buys
 * only a consent submission for an address already proven, and it is spent
 * within the minute — but "small credential" is not "not a credential".
 *
 * The cookie is the TRANSPORT and not the state. The sign-in page reads it once,
 * hands the outcome to the form as an ordinary prop, and the form holds it in
 * component state exactly as it holds the passcode door's — one shape, one
 * consent step, one submission endpoint. The submission route erases the cookie
 * on its way out, so a token that has been spent cannot be handed to a second
 * render of the step.
 *
 * The ".ts" is written out in the local import because the unit tests run this
 * module directly under `node --experimental-strip-types`, which resolves
 * specifiers exactly. Next resolves it identically.
 */

import type { ConsentRequired } from "./customer-session.ts";

/** Name of the cookie carrying one held sign-in across the callback redirect. */
export const PENDING_CONSENT_COOKIE = "ticket_pos_pending_consent";

/**
 * Fifteen minutes, matching the server's own life for a pending-consent token
 * (backend consentgate.go). The two are deliberately the same number: a cookie
 * that outlived the token would offer a consent step that cannot be completed,
 * and one that died first would lose a step the API is still willing to honour.
 *
 * It expires on its own as a backstop only. The ordinary end of this cookie is
 * the consent submission deleting it, whatever the submission's outcome.
 */
const PENDING_CONSENT_MAX_AGE_SECONDS = 15 * 60;

/**
 * Path "/" rather than the narrow scoping the Google state cookie gets, and the
 * reason is that the reader is a PAGE: /{locale}/signin, whose address carries a
 * language and therefore cannot be written down as one constant path the way
 * /api/customer/auth/google/callback can. The compensation is that it is
 * httpOnly, it lives fifteen minutes, and it is deleted by the route that spends
 * it.
 */
export function pendingConsentCookieOptions() {
  return {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    // Lax: the cookie is SET on a response to Google's cross-site top-level GET
    // and read on an ordinary same-site navigation immediately after.
    sameSite: "lax" as const,
    path: "/",
    maxAge: PENDING_CONSENT_MAX_AGE_SECONDS,
  };
}

/** The attributes that erase it. Path must match to delete it. */
export function clearedPendingConsentCookieOptions() {
  return { ...pendingConsentCookieOptions(), maxAge: 0 };
}

/**
 * Serialises a consent-required outcome for the cookie: JSON, base64url so it
 * needs no quoting — the same shape the Google state cookie travels in.
 *
 * The field names are short for the same reason that one's are: this is a
 * cookie on every request to this origin for the next fifteen minutes.
 */
export function encodePendingConsent(consent: ConsentRequired): string {
  return base64UrlEncode(
    new TextEncoder().encode(
      JSON.stringify({
        t: consent.pending_consent_token,
        x: consent.expires_at,
        b: [
          consent.boxes.policy_acceptance,
          consent.boxes.marketing_consent,
          consent.boxes.networking_consent,
          consent.boxes.terms_acceptance,
        ],
      }),
    ),
  );
}

/**
 * Reads the cookie back, returning null for anything that is not a complete
 * held sign-in — absent, truncated, not base64url, not JSON, missing the token.
 *
 * Null is the ordinary case, not an error: it is what every visitor who did not
 * arrive from a held Google Sign-In has. The sign-in page renders its email step
 * for it, which is exactly right.
 *
 * The boxes are read as booleans and defaulted to FALSE rather than to true.
 * A damaged cookie must not be able to conjure a checkbox: which boxes a
 * Customer is owed is the API's finding, and the API recomputes it at the write
 * in any case (SubmitConsent), so a box shown that should not have been changes
 * nothing but what somebody was asked. The required box is the exception — this
 * outcome exists precisely because Policy Acceptance is outstanding — and it is
 * still read rather than assumed, because a cookie that cannot say so is a
 * cookie this app should not be acting on at all.
 */
export function decodePendingConsent(raw: string | null | undefined): ConsentRequired | null {
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
  const { t, x, b } = parsed as { t?: unknown; x?: unknown; b?: unknown };
  if (typeof t !== "string" || !t) {
    return null;
  }
  const boxes = Array.isArray(b) ? b : [];
  if (boxes[0] !== true && boxes[3] !== true) {
    // No required box — neither the Policy Acceptance nor the Terms (#536) —
    // means no consent step. The form would render a submit button nothing
    // could enable, which is a dead end rather than a step.
    return null;
  }
  return {
    pending_consent_token: t,
    expires_at: typeof x === "string" ? x : "",
    boxes: {
      policy_acceptance: boxes[0] === true,
      marketing_consent: boxes[1] === true,
      networking_consent: boxes[2] === true,
      terms_acceptance: boxes[3] === true,
    },
  };
}

// --- base64url ------------------------------------------------------------
//
// btoa/atob rather than Buffer, matching lib/google-signin.ts: both exist in
// every runtime this module can be bundled for, and Buffer does not.

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
