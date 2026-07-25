import assert from "node:assert/strict";
import test from "node:test";

import {
  GOOGLE_SIGN_IN_FAILURE_PATH,
  GOOGLE_STATE_COOKIE_PATH,
  authorizationUrl,
  clearedGoogleStateCookieOptions,
  codeChallengeS256,
  decodePendingSignIn,
  encodePendingSignIn,
  googleSignInConfig,
  googleStateCookieOptions,
  isGoogleSignInConfigured,
  newPendingSignIn,
  randomToken,
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
  const first = newPendingSignIn();
  const second = newPendingSignIn();

  assert.notEqual(first.state, second.state);
  assert.notEqual(first.codeVerifier, second.codeVerifier);
  assert.notEqual(first.state, first.codeVerifier);
  // 32 bytes as base64url, which is 43 characters and inside RFC 7636's 43-128.
  for (const value of [first.state, first.codeVerifier]) {
    assert.equal(value.length, 43);
    assert.match(value, /^[A-Za-z0-9_-]+$/);
  }
});

test("randomToken is base64url with no padding", () => {
  assert.match(randomToken(), /^[A-Za-z0-9_-]{43}$/);
});

test("codeChallengeS256 is the RFC 7636 example, so the digest is not merely self-consistent", async () => {
  // RFC 7636 appendix B.
  const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
  assert.equal(await codeChallengeS256(verifier), "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM");
});

test("the authorization URL asks for openid email only, with S256 PKCE and an account picker", async () => {
  const url = new URL(
    authorizationUrl(
      { clientId: "staff-client-id", redirectUri: "https://pos.example/api/auth/google/callback" },
      { state: "the-state", codeChallenge: "the-challenge" },
    ),
  );

  assert.equal(url.origin + url.pathname, "https://accounts.google.com/o/oauth2/v2/auth");
  assert.equal(url.searchParams.get("client_id"), "staff-client-id");
  assert.equal(
    url.searchParams.get("redirect_uri"),
    "https://pos.example/api/auth/google/callback",
  );
  assert.equal(url.searchParams.get("response_type"), "code");
  // Not `profile`: nothing here stores anything Google returns (PRD decision 4).
  assert.equal(url.searchParams.get("scope"), "openid email");
  assert.equal(url.searchParams.get("state"), "the-state");
  assert.equal(url.searchParams.get("code_challenge"), "the-challenge");
  assert.equal(url.searchParams.get("code_challenge_method"), "S256");
  assert.equal(url.searchParams.get("prompt"), "select_account");
});

test("the verifier never travels to Google — only its digest does", async () => {
  const pending = newPendingSignIn();
  const url = authorizationUrl(
    { clientId: "id", redirectUri: "https://pos.example/api/auth/google/callback" },
    { state: pending.state, codeChallenge: await codeChallengeS256(pending.codeVerifier) },
  );
  assert.ok(!url.includes(pending.codeVerifier));
});

// --- state comparison -----------------------------------------------------

test("statesMatch accepts only an exact match", () => {
  assert.equal(statesMatch("abc123", "abc123"), true);
  assert.equal(statesMatch("abc123", "abc124"), false);
  assert.equal(statesMatch("abc123", "abc1234"), false);
  assert.equal(statesMatch("abc123", "abc12"), false);
});

test("statesMatch treats a missing or empty state as a mismatch, never a pass", () => {
  assert.equal(statesMatch("abc123", null), false);
  assert.equal(statesMatch("abc123", undefined), false);
  assert.equal(statesMatch("abc123", ""), false);
  assert.equal(statesMatch("", ""), false);
  assert.equal(statesMatch("", "anything"), false);
});

// --- the state cookie -----------------------------------------------------

test("a pending sign-in survives a round-trip through the cookie", () => {
  const pending = newPendingSignIn();
  const decoded = decodePendingSignIn(encodePendingSignIn(pending));
  assert.deepEqual(decoded, pending);
});

test("the encoded cookie needs no quoting", () => {
  assert.match(encodePendingSignIn(newPendingSignIn()), /^[A-Za-z0-9_-]+$/);
});

test("decodePendingSignIn returns null for anything that is not one", () => {
  assert.equal(decodePendingSignIn(null), null);
  assert.equal(decodePendingSignIn(undefined), null);
  assert.equal(decodePendingSignIn(""), null);
  assert.equal(decodePendingSignIn("not base64url!!"), null);
  assert.equal(decodePendingSignIn(btoa("plain text")), null);
  // Well-formed JSON, but not a complete pending sign-in.
  assert.equal(decodePendingSignIn(encodeJson({ s: "state-only" })), null);
  assert.equal(decodePendingSignIn(encodeJson({ v: "verifier-only" })), null);
  assert.equal(decodePendingSignIn(encodeJson({ s: "", v: "" })), null);
  assert.equal(decodePendingSignIn(encodeJson({ s: 1, v: 2 })), null);
  assert.equal(decodePendingSignIn(encodeJson(null)), null);
});

test("the state cookie is httpOnly, Lax, and scoped to the callback route alone", () => {
  const options = googleStateCookieOptions();
  assert.equal(options.httpOnly, true);
  // Lax rather than Strict: Google's callback is a cross-site top-level GET.
  assert.equal(options.sameSite, "lax");
  assert.equal(options.path, GOOGLE_STATE_COOKIE_PATH);
  assert.equal(options.path, "/api/auth/google/callback");
  // Ten minutes: long enough for a consent screen, short enough to matter.
  assert.equal(options.maxAge, 10 * 60);
});

test("the state cookie is Secure in production and not on localhost http", () => {
  withEnv({ NODE_ENV: "production" }, () => {
    assert.equal(googleStateCookieOptions().secure, true);
  });
  withEnv({ NODE_ENV: "development" }, () => {
    assert.equal(googleStateCookieOptions().secure, false);
  });
});

test("clearing the cookie keeps the path, or the browser would not delete it", () => {
  const cleared = clearedGoogleStateCookieOptions();
  assert.equal(cleared.path, GOOGLE_STATE_COOKIE_PATH);
  assert.equal(cleared.maxAge, 0);
});

// --- configuration --------------------------------------------------------

test("Google is configured only when both halves are present", () => {
  withEnv(
    { GOOGLE_CLIENT_ID: "staff-id", GOOGLE_REDIRECT_URI: "https://pos.example/cb" },
    () => {
      assert.equal(isGoogleSignInConfigured(), true);
      assert.deepEqual(googleSignInConfig(), {
        clientId: "staff-id",
        redirectUri: "https://pos.example/cb",
      });
    },
  );

  // A missing, empty, or whitespace-only half means no button rather than a
  // broken one (PRD "Local development").
  for (const vars of [
    { GOOGLE_CLIENT_ID: "staff-id", GOOGLE_REDIRECT_URI: undefined },
    { GOOGLE_CLIENT_ID: undefined, GOOGLE_REDIRECT_URI: "https://pos.example/cb" },
    { GOOGLE_CLIENT_ID: "  ", GOOGLE_REDIRECT_URI: "https://pos.example/cb" },
    { GOOGLE_CLIENT_ID: undefined, GOOGLE_REDIRECT_URI: undefined },
  ]) {
    withEnv(vars, () => {
      assert.equal(isGoogleSignInConfigured(), false);
      assert.equal(googleSignInConfig(), null);
    });
  }
});

// --- failure ---------------------------------------------------------------

test("every failure lands on the login page, which still has a passcode form", () => {
  assert.ok(GOOGLE_SIGN_IN_FAILURE_PATH.startsWith("/login"));
  // One marker for every cause. Nothing in it distinguishes an unknown address
  // from a cancelled consent screen or a refused exchange (PRD decision 9).
  assert.equal(new URL(GOOGLE_SIGN_IN_FAILURE_PATH, "https://pos.example").search, "?google=failed");
});

function encodeJson(value: unknown): string {
  return btoa(JSON.stringify(value)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
