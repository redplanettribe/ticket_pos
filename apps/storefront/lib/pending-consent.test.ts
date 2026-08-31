import assert from "node:assert/strict";
import test from "node:test";

import type { ConsentRequired } from "./customer-session.ts";
import {
  PENDING_CONSENT_COOKIE,
  clearedPendingConsentCookieOptions,
  decodePendingConsent,
  encodePendingConsent,
  pendingConsentCookieOptions,
} from "./pending-consent.ts";

// The cookie that carries a held Google Sign-In across the callback redirect
// (#252). Everything here is about what survives the round trip and what a
// damaged or forged cookie is allowed to become — the same discipline the Google
// state cookie's tests apply, for the same reason: a cookie is not a place to
// start trusting data from.

const held: ConsentRequired = {
  pending_consent_token: "b2b0f5c1d6e74a9c8f1e2d3c4b5a69788796a5b4c3d2e1f0a9b8c7d6e5f40312",
  expires_at: "2026-08-11T18:30:00Z",
  boxes: {
    policy_acceptance: true,
    marketing_consent: true,
    networking_consent: true,
    terms_acceptance: true,
  },
};

test("a held sign-in survives the redirect exactly as it went in", () => {
  assert.deepEqual(decodePendingConsent(encodePendingConsent(held)), held);
});

test("the boxes the API asked for are the boxes that come back", () => {
  // A Customer re-prompted by a version bump is owed the required box ALONE
  // (user story 22). The cookie must not widen that back out.
  const requiredOnly: ConsentRequired = {
    ...held,
    boxes: {
      policy_acceptance: true,
      marketing_consent: false,
      networking_consent: false,
      terms_acceptance: false,
    },
  };
  assert.deepEqual(decodePendingConsent(encodePendingConsent(requiredOnly)), requiredOnly);

  // A terms-only re-gate (#536) is the mirror case: a later Terms edition owes
  // its box alone, and the cookie must carry that step without conjuring the
  // policy box back.
  const termsOnly: ConsentRequired = {
    ...held,
    boxes: {
      policy_acceptance: false,
      marketing_consent: false,
      networking_consent: false,
      terms_acceptance: true,
    },
  };
  assert.deepEqual(decodePendingConsent(encodePendingConsent(termsOnly)), termsOnly);
});

test("the token never appears in the cookie in the clear", () => {
  // base64url, so it is not readable at a glance in a devtools cookie list.
  // This is tidiness and emphatically not protection — the value is httpOnly,
  // which is what protects it — but a credential written out in plain sight in
  // a cookie jar is an invitation to copy it somewhere worse.
  assert.ok(!encodePendingConsent(held).includes(held.pending_consent_token));
});

test("nothing at all is a visitor who did not come from a held sign-in", () => {
  assert.equal(decodePendingConsent(undefined), null);
  assert.equal(decodePendingConsent(null), null);
  assert.equal(decodePendingConsent(""), null);
});

test("a damaged cookie is no consent step rather than a broken one", () => {
  assert.equal(decodePendingConsent("not-base64url-!!"), null);
  assert.equal(decodePendingConsent(btoa("not json at all")), null);
  assert.equal(decodePendingConsent(btoa("[1,2,3]")), null);
  assert.equal(decodePendingConsent(btoa('"a string"')), null);
});

test("a cookie with no token buys nothing", () => {
  assert.equal(decodePendingConsent(btoa(JSON.stringify({ x: "later", b: [true] }))), null);
  assert.equal(
    decodePendingConsent(btoa(JSON.stringify({ t: "", x: "later", b: [true] }))),
    null,
  );
  assert.equal(decodePendingConsent(btoa(JSON.stringify({ t: 42, b: [true] }))), null);
});

test("a cookie that does not claim a required box is not a consent step", () => {
  // The consent-required outcome exists precisely because a required acceptance
  // — the Policy's or the Terms' (#536) — is outstanding, so a cookie claiming
  // neither is not one. Rendering it would put a submit button on screen that
  // nothing could ever enable.
  assert.equal(decodePendingConsent(btoa(JSON.stringify({ t: "abc", b: [] }))), null);
  assert.equal(decodePendingConsent(btoa(JSON.stringify({ t: "abc", b: [false, true] }))), null);
  assert.equal(decodePendingConsent(btoa(JSON.stringify({ t: "abc" }))), null);
});

test("optional boxes default to not shown, never to shown", () => {
  // A tampered cookie can only ever cost somebody a question they did not owe,
  // and cannot even do that: the API recomputes which boxes are outstanding at
  // the write and ignores answers to any others.
  const decoded = decodePendingConsent(btoa(JSON.stringify({ t: "abc", b: [true, "yes", 1] })));
  assert.deepEqual(decoded?.boxes, {
    policy_acceptance: true,
    marketing_consent: false,
    networking_consent: false,
    terms_acceptance: false,
  });
});

test("a missing expiry decodes to empty rather than to a lie about the time", () => {
  const decoded = decodePendingConsent(btoa(JSON.stringify({ t: "abc", b: [true, true, true] })));
  assert.equal(decoded?.expires_at, "");
});

// --- the cookie itself ----------------------------------------------------

test("the cookie is httpOnly and site-wide, so only the sign-in page can read it", () => {
  const options = pendingConsentCookieOptions();
  assert.equal(options.httpOnly, true);
  assert.equal(options.sameSite, "lax");
  // Site-wide because its reader is /{locale}/signin, whose address carries a
  // language and is therefore not one constant path.
  assert.equal(options.path, "/");
  // Fifteen minutes, the same life the API gives the token inside it.
  assert.equal(options.maxAge, 15 * 60);
});

test("it is named apart from the session cookie it is deliberately not", () => {
  assert.equal(PENDING_CONSENT_COOKIE, "ticket_pos_pending_consent");
  assert.notEqual(PENDING_CONSENT_COOKIE, "ticket_pos_customer_session");
});

test("clearing it matches the path it was written at, or it would not clear", () => {
  const cleared = clearedPendingConsentCookieOptions();
  assert.equal(cleared.maxAge, 0);
  assert.equal(cleared.path, pendingConsentCookieOptions().path);
});
