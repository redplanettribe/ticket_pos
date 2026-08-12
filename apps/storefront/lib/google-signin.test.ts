import assert from "node:assert/strict";
import test from "node:test";

import { DEFAULT_DESTINATION } from "./destination.ts";
import {
  GOOGLE_STATE_COOKIE_PATH,
  authorizationUrl,
  clearedGoogleStateCookieOptions,
  codeChallengeS256,
  decodePendingSignIn,
  encodePendingSignIn,
  googleSignInConfig,
  googleSignInStartPath,
  googleStateCookieOptions,
  isGoogleSignInConfigured,
  newPendingSignIn,
  randomToken,
  signInConsentPath,
  signInFailurePath,
  statesMatch,
} from "./google-signin.ts";

const env = process.env as Record<string, string | undefined>;

/** Runs body with the given environment, restoring whatever was there before. */
function withEnv(vars: Record<string, string | undefined>, body: () => void) {
  const previous = new Map(Object.keys(vars).map((key) => [key, env[key]]));
  for (const [key, value] of Object.entries(vars)) {
    if (value === undefined) delete env[key];
    else env[key] = value;
  }
  try {
    body();
  } finally {
    for (const [key, value] of previous) {
      if (value === undefined) delete env[key];
      else env[key] = value;
    }
  }
}

// --- state and PKCE -------------------------------------------------------

test("newPendingSignIn mints an unguessable state and verifier, different every time", () => {
  const first = newPendingSignIn("/tickets");
  const second = newPendingSignIn("/tickets");

  assert.notEqual(first.state, second.state);
  assert.notEqual(first.codeVerifier, second.codeVerifier);
  assert.notEqual(first.state, first.codeVerifier);
  // 32 bytes as base64url, which is 43 characters and inside RFC 7636's 43-128.
  for (const value of [first.state, first.codeVerifier]) {
    assert.equal(value.length, 43);
    assert.match(value, /^[A-Za-z0-9_-]+$/);
  }
});

test("newPendingSignIn guards the destination it is given", () => {
  assert.equal(newPendingSignIn("/rock-fest/events/gig").destination, "/rock-fest/events/gig");
  assert.equal(newPendingSignIn("https://evil.example").destination, DEFAULT_DESTINATION);
});

test("randomToken produces base64url of the requested length", () => {
  assert.match(randomToken(16), /^[A-Za-z0-9_-]{22}$/);
  assert.notEqual(randomToken(), randomToken());
});

test("codeChallengeS256 matches the RFC 7636 worked example", async () => {
  // Appendix B of RFC 7636: this verifier hashes to this challenge.
  const challenge = await codeChallengeS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk");
  assert.equal(challenge, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM");
});

test("statesMatch accepts only the exact nonce that was issued", () => {
  const { state } = newPendingSignIn("/tickets");

  assert.equal(statesMatch(state, state), true);
  assert.equal(statesMatch(state, `${state}x`), false);
  assert.equal(statesMatch(state, state.slice(0, -1)), false);
  assert.equal(statesMatch(state, `${state.slice(0, -1)}!`), false);
});

test("statesMatch treats a missing nonce on either side as a mismatch", () => {
  assert.equal(statesMatch("abc", null), false);
  assert.equal(statesMatch("abc", undefined), false);
  assert.equal(statesMatch("abc", ""), false);
  assert.equal(statesMatch("", ""), false);
});

// --- the state cookie -----------------------------------------------------

test("the state cookie round-trips a pending sign-in", () => {
  const pending = newPendingSignIn("/rock-fest/events/summer-night");
  const decoded = decodePendingSignIn(encodePendingSignIn(pending));

  assert.deepEqual(decoded, pending);
});

test("the encoded cookie needs no quoting and does not spell out its contents", () => {
  const pending = newPendingSignIn("/tickets");
  const encoded = encodePendingSignIn(pending);

  assert.match(encoded, /^[A-Za-z0-9_-]+$/);
});

test("decodePendingSignIn refuses anything that is not a complete pending sign-in", () => {
  assert.equal(decodePendingSignIn(undefined), null, "no cookie at all");
  assert.equal(decodePendingSignIn(null), null);
  assert.equal(decodePendingSignIn(""), null, "an expired cookie sent empty");
  assert.equal(decodePendingSignIn("not-base64url!!"), null);
  assert.equal(decodePendingSignIn(encodeJson("not an object")), null);
  assert.equal(decodePendingSignIn(encodeJson({ s: "state", d: "/tickets" })), null, "no verifier");
  assert.equal(decodePendingSignIn(encodeJson({ v: "verifier", d: "/tickets" })), null, "no state");
  assert.equal(decodePendingSignIn(encodeJson({ s: "", v: "verifier" })), null, "empty state");
  assert.equal(decodePendingSignIn(encodeJson({ s: 1, v: 2 })), null, "wrong types");
});

test("decodePendingSignIn runs safeNext over the destination it reads back", () => {
  // Even a forged cookie cannot redirect anybody off this Storefront.
  const forged = encodeJson({ s: "state", v: "verifier", d: "https://evil.example/steal" });
  assert.equal(decodePendingSignIn(forged)?.destination, DEFAULT_DESTINATION);

  const noDestination = encodeJson({ s: "state", v: "verifier" });
  assert.equal(decodePendingSignIn(noDestination)?.destination, DEFAULT_DESTINATION);
});

test("the state cookie is httpOnly, SameSite=Lax, ten minutes, and scoped to the callback", () => {
  const options = googleStateCookieOptions();

  assert.equal(options.httpOnly, true);
  assert.equal(options.sameSite, "lax");
  assert.equal(options.path, GOOGLE_STATE_COOKIE_PATH);
  assert.equal(options.path, "/api/customer/auth/google/callback");
  assert.equal(options.maxAge, 10 * 60);
});

test("the state cookie is Secure in production and not in local development", () => {
  withEnv({ NODE_ENV: "production" }, () => {
    assert.equal(googleStateCookieOptions().secure, true);
  });
  withEnv({ NODE_ENV: "development" }, () => {
    // http://localhost has no TLS; Secure there would drop the cookie entirely.
    assert.equal(googleStateCookieOptions().secure, false);
  });
});

test("clearing the state cookie keeps the path, so the browser actually deletes it", () => {
  const cleared = clearedGoogleStateCookieOptions();

  assert.equal(cleared.maxAge, 0);
  assert.equal(cleared.path, googleStateCookieOptions().path);
  assert.equal(cleared.httpOnly, true);
});

// --- configuration --------------------------------------------------------

test("Google Sign-In is unconfigured unless both the client ID and redirect URI are set", () => {
  withEnv({ GOOGLE_CLIENT_ID: undefined, GOOGLE_REDIRECT_URI: undefined }, () => {
    assert.equal(googleSignInConfig(), null);
    assert.equal(isGoogleSignInConfigured(), false);
  });
  withEnv({ GOOGLE_CLIENT_ID: "client-id", GOOGLE_REDIRECT_URI: undefined }, () => {
    assert.equal(googleSignInConfig(), null);
  });
  withEnv({ GOOGLE_CLIENT_ID: "  ", GOOGLE_REDIRECT_URI: "https://tickets.example/cb" }, () => {
    // An empty value in a .env file is the ordinary way to have no credentials.
    assert.equal(googleSignInConfig(), null);
  });
  withEnv({ GOOGLE_CLIENT_ID: " client-id ", GOOGLE_REDIRECT_URI: " https://x/cb " }, () => {
    assert.deepEqual(googleSignInConfig(), { clientId: "client-id", redirectUri: "https://x/cb" });
    assert.equal(isGoogleSignInConfigured(), true);
  });
});

// --- the authorization URL ------------------------------------------------

test("the authorization URL asks Google for an email, a profile picture, and an account picker", () => {
  const url = new URL(
    authorizationUrl(
      { clientId: "client-id", redirectUri: "https://tickets.example/api/customer/auth/google/callback" },
      { state: "the-state", codeChallenge: "the-challenge" },
    ),
  );

  assert.equal(url.origin + url.pathname, "https://accounts.google.com/o/oauth2/v2/auth");
  assert.equal(url.searchParams.get("client_id"), "client-id");
  assert.equal(
    url.searchParams.get("redirect_uri"),
    "https://tickets.example/api/customer/auth/google/callback",
  );
  assert.equal(url.searchParams.get("response_type"), "code");
  // `profile` is here for the `picture` claim alone, which seeds a Customer
  // Avatar; the names it also offers stay unread (see SCOPE in google-signin.ts).
  assert.equal(url.searchParams.get("scope"), "openid email profile");
  assert.equal(url.searchParams.get("state"), "the-state");
  assert.equal(url.searchParams.get("code_challenge"), "the-challenge");
  assert.equal(url.searchParams.get("code_challenge_method"), "S256");
  assert.equal(url.searchParams.get("prompt"), "select_account");
});

test("the authorization URL never carries the destination or the verifier", () => {
  const pending = newPendingSignIn("/rock-fest/events/summer-night");
  const url = authorizationUrl(
    { clientId: "client-id", redirectUri: "https://tickets.example/cb" },
    { state: pending.state, codeChallenge: "the-challenge" },
  );

  // The destination stays in the cookie on our origin, out of Google's logs.
  assert.equal(url.includes("summer-night"), false);
  assert.equal(url.includes(pending.codeVerifier), false);
});

// --- destinations and failures -------------------------------------------

test("the Google button carries the destination the visitor came for", () => {
  assert.equal(googleSignInStartPath("/tickets"), "/api/customer/auth/google/start");
  assert.equal(
    googleSignInStartPath("/rock-fest/events/summer-night"),
    "/api/customer/auth/google/start?next=%2Frock-fest%2Fevents%2Fsummer-night",
  );
  assert.equal(googleSignInStartPath("https://evil.example"), "/api/customer/auth/google/start");
});

test("every failure lands on the sign-in page with one generic message", () => {
  // Cancelled consent screen, state mismatch, missing cookie, refused exchange:
  // the callback has one answer for all of them, and this is it.
  assert.equal(signInFailurePath("/tickets"), "/signin?google=failed");
});

test("a failure keeps the destination so the passcode form still returns them", () => {
  assert.equal(
    signInFailurePath("/rock-fest/events/summer-night"),
    "/signin?google=failed&next=%2Frock-fest%2Fevents%2Fsummer-night",
  );
});

test("a failure never carries a destination off this Storefront", () => {
  assert.equal(signInFailurePath("//evil.example"), "/signin?google=failed");
});

// --- the consent step across the callback redirect (#252) ------------------

test("a held sign-in lands on the consent step, not on the failure message", () => {
  // The opposite outcome from signInFailurePath above, and the difference is the
  // whole of #252: this visitor's address WAS proven, so telling them the
  // sign-in failed would be false, and sending them back to a passcode form
  // would charge them for a door they already came through (ADR 0011).
  assert.equal(signInConsentPath(DEFAULT_DESTINATION), "/signin?consent=pending");
  assert.notEqual(signInConsentPath(DEFAULT_DESTINATION), signInFailurePath(DEFAULT_DESTINATION));
});

test("the consent step keeps the destination the visitor was heading for", () => {
  assert.equal(
    signInConsentPath("/rock-fest/events/summer-night"),
    "/signin?consent=pending&next=%2Frock-fest%2Fevents%2Fsummer-night",
  );
});

test("the consent step keeps the Follow pressed before signing in", () => {
  // Being asked about consent must not cost somebody the thing they came to do
  // (#219). The form relays the intent on the submission, which is the request
  // that finally mints a session for it to be written against.
  assert.equal(
    signInConsentPath("/rock-fest", "organization:rock-fest"),
    "/signin?consent=pending&next=%2Frock-fest&follow=organization%3Arock-fest",
  );
});

test("the consent step never carries a destination or an intent it should not", () => {
  assert.equal(signInConsentPath("//evil.example"), "/signin?consent=pending");
  assert.equal(
    signInConsentPath("/rock-fest", "javascript:alert(1)"),
    "/signin?consent=pending&next=%2Frock-fest",
  );
});

test("no credential is ever written into the address", () => {
  // The pending-consent token travels in an httpOnly cookie and nowhere else. A
  // query parameter would end up in browser history, in this app's own logs, and
  // in the Referer of the Privacy Policy link the consent step opens in a new
  // tab. `consent=pending` says only "look for a held sign-in".
  const path = signInConsentPath("/rock-fest", "organization:rock-fest");
  const params = new URLSearchParams(path.slice(path.indexOf("?")));
  assert.deepEqual([...params.keys()].sort(), ["consent", "follow", "next"]);
  assert.equal(params.get("consent"), "pending");
});

// --- the Follow intent across the Google leg (#219) ------------------------

test("a Follow intent crosses the redirect in the cookie, never in Google's URL", () => {
  const pending = newPendingSignIn("/rock-fest", "organization:rock-fest");
  assert.equal(pending.followIntent, "organization:rock-fest");
  assert.deepEqual(decodePendingSignIn(encodePendingSignIn(pending)), pending);

  // Google is told the nonce and the challenge and nothing else about this
  // sign-in: the intent stays on this origin, out of Google's logs.
  const url = authorizationUrl(
    { clientId: "client", redirectUri: "https://storefront.example/callback" },
    { state: pending.state, codeChallenge: "challenge" },
  );
  assert.equal(url.includes("rock-fest"), false);
});

test("a forged or absent intent in the cookie decodes to none", () => {
  assert.equal(newPendingSignIn("/rock-fest").followIntent, null);
  assert.equal(newPendingSignIn("/rock-fest", "playlist:rock-fest").followIntent, null);
  // A cookie is not a place to start trusting text from: the guard runs again on
  // the way out.
  assert.equal(
    decodePendingSignIn(
      encodeJson({ s: "state", v: "verifier", d: "/rock-fest", f: "organization:ROCK FEST" }),
    )?.followIntent,
    null,
  );
});

test("the Google button and the failure path both carry the intent", () => {
  assert.equal(
    googleSignInStartPath("/rock-fest", "organization:rock-fest"),
    "/api/customer/auth/google/start?next=%2Frock-fest&follow=organization%3Arock-fest",
  );
  // Even when the destination is the default and would otherwise be dropped.
  assert.equal(
    googleSignInStartPath("/tickets", "organization:rock-fest"),
    "/api/customer/auth/google/start?follow=organization%3Arock-fest",
  );
  // Falling back to the passcode form must not cost the visitor what they
  // pressed.
  assert.equal(
    signInFailurePath("/rock-fest", "organization:rock-fest"),
    "/signin?google=failed&next=%2Frock-fest&follow=organization%3Arock-fest",
  );
  assert.equal(signInFailurePath("/tickets", "playlist:rock-fest"), "/signin?google=failed");
});

/** Encodes an arbitrary value the way the state cookie is encoded, for the refusal cases. */
function encodeJson(value: unknown): string {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  let binary = "";
  for (let i = 0; i < bytes.length; i += 1) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
