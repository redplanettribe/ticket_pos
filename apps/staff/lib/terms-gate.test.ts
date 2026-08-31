import assert from "node:assert/strict";
import test from "node:test";

import {
  RETURN_PARAM,
  TERMS_GATE_PATH,
  decideTermsGate,
  termsGateReturnPath,
} from "./terms-gate.ts";

/**
 * The staff Terms gate's decision (#570, ADR 0067), tested where it is: a pure
 * function, run under plain node.
 *
 * This is deliberately not a Playwright test. The e2e suite cannot be green in
 * one run — passcodes are rationed to 10 per IP per 15 minutes and the suite
 * needs more sign-ins than that — and it runs against a stale dev stack that
 * builds nothing, so a browser test here would assert about a build nobody
 * made. The other half of the cover is at the Go HTTP seam
 * (backend/integration/staff_terms_live_session_test.go), which answers the
 * question this file cannot: does the predicate bite for this session against
 * this floor?
 */

test("an outstanding acceptance diverts a navigation to the interstitial", () => {
  const decision = decideTermsGate({ pathname: "/events", termsOutstanding: true });
  assert.deepEqual(decision, {
    kind: "interstitial",
    pathname: TERMS_GATE_PATH,
    search: `?${RETURN_PARAM}=${encodeURIComponent("/events")}`,
  });
});

test("the diversion remembers the query it was carrying", () => {
  const decision = decideTermsGate({
    pathname: "/events/abc/sales",
    search: "?tab=manual&page=2",
    termsOutstanding: true,
  });
  assert.equal(decision.kind, "interstitial");
  assert.equal(
    decision.kind === "interstitial" ? decision.search : "",
    `?${RETURN_PARAM}=${encodeURIComponent("/events/abc/sales?tab=manual&page=2")}`,
  );
});

// The publishing operator meets their own interstitial: the person who
// re-gated everybody has demonstrably read the text they published.
test("the operator surface is gated like everything else", () => {
  assert.equal(decideTermsGate({ pathname: "/operator/legal", termsOutstanding: true }).kind, "interstitial");
});

// The gate is a navigation gate and nothing else. A sale in progress commits,
// and no in-flight mutation is refused because an edition rolled over.
test("no /api/ path is ever gated, whatever is outstanding", () => {
  for (const pathname of [
    "/api/auth/session",
    "/api/events/abc/sales",
    "/api/auth/terms-gate/accept",
  ]) {
    assert.equal(decideTermsGate({ pathname, termsOutstanding: true }).kind, "allow", pathname);
  }
});

test("the interstitial and the sign-in page are not diverted into themselves", () => {
  for (const pathname of [TERMS_GATE_PATH, `${TERMS_GATE_PATH}/anything`, "/login"]) {
    assert.equal(decideTermsGate({ pathname, termsOutstanding: true }).kind, "allow", pathname);
  }
});

test("a path merely starting with the gate's letters is still gated", () => {
  assert.equal(decideTermsGate({ pathname: "/termsomething", termsOutstanding: true }).kind, "interstitial");
});

// Only an explicit `true` diverts. Null is the backend's "not asked", and also
// what it reports when the consent read FAILED — where diverting would strand
// everybody in front of an interstitial that cannot be rendered.
test("nothing but an explicit true diverts anybody", () => {
  for (const termsOutstanding of [false, null, undefined]) {
    assert.equal(
      decideTermsGate({ pathname: "/events", termsOutstanding }).kind,
      "allow",
      String(termsOutstanding),
    );
  }
});

test("accepting returns the person to where they were going", () => {
  assert.equal(termsGateReturnPath("/events/abc/sales?tab=manual"), "/events/abc/sales?tab=manual");
  assert.equal(termsGateReturnPath("/pos"), "/pos");
});

test("a missing or unusable return path lands on the app root", () => {
  for (const next of [null, undefined, "", "/", "events", "https://evil.test/steal"]) {
    assert.equal(termsGateReturnPath(next), "/", String(next));
  }
});

// The parameter is in a URL somebody can be handed, so it is a claim and not a
// destination: an open redirect out of the staff app is the thing to prevent.
test("a return path that leaves the site is refused", () => {
  for (const next of ["//evil.test/steal", "/\\evil.test", "/events\nLocation: https://evil.test"]) {
    assert.equal(termsGateReturnPath(next), "/", next);
  }
});

test("a return path into the gate or the API lands on the app root instead", () => {
  for (const next of [TERMS_GATE_PATH, `${TERMS_GATE_PATH}?next=/x`, "/api/auth/session"]) {
    assert.equal(termsGateReturnPath(next), "/", next);
  }
});

// The round trip is what the two functions owe each other: whatever the
// diversion encoded, the return decodes back to the address that was asked for.
test("a diverted address survives the round trip", () => {
  const asked = "/events/abc/sales?tab=manual&q=a%20b";
  const decision = decideTermsGate({
    pathname: "/events/abc/sales",
    search: "?tab=manual&q=a%20b",
    termsOutstanding: true,
  });
  assert.equal(decision.kind, "interstitial");
  const encoded = decision.kind === "interstitial" ? decision.search : "";
  const next = new URLSearchParams(encoded).get(RETURN_PARAM);
  assert.equal(termsGateReturnPath(next), asked);
});
