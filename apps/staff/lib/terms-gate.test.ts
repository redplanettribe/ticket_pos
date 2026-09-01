import assert from "node:assert/strict";
import test from "node:test";

import {
  RETURN_PARAM,
  TERMS_GATE_PATH,
  asksAdulthoodDeclaration,
  decideTermsGate,
  termsGateAnswersComplete,
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

// The Adulthood Declaration's half of the gate (#587, ADR 0069): which boxes
// are owed, and when the submit button may be pressed.
//
// Extracted here rather than tested in a component for the file's whole reason:
// no component-testing stack exists in this repo and none is being introduced,
// and the alternative — Playwright — cannot be green in one run. So the
// rendering DECISION lives in a pure function and the two forms only draw it.

test("the label's presence is the whole of whether the second box is drawn", () => {
  assert.equal(asksAdulthoodDeclaration("Declaro ser mayor de edad"), true);
  // Absent from the payload: this edition carries no artifact and asks nothing.
  for (const label of [undefined, null, ""]) {
    assert.equal(asksAdulthoodDeclaration(label), false, String(label));
  }
  // A box with nothing written beside it is the one thing §3 forbids, so
  // whitespace is not words.
  assert.equal(asksAdulthoodDeclaration("   \n "), false);
});

test("an edition that does not ask is satisfied by the Terms box alone", () => {
  assert.equal(
    termsGateAnswersComplete({ termsAccepted: true, adulthoodDeclared: false }),
    true,
  );
  assert.equal(
    termsGateAnswersComplete({ termsAccepted: false, adulthoodDeclared: true }),
    false,
  );
});

test("an edition that asks owes both boxes before the button may be pressed", () => {
  const label = "I declare that I am eighteen years of age or older";
  assert.equal(
    termsGateAnswersComplete({
      adulthoodDeclarationLabel: label,
      termsAccepted: true,
      adulthoodDeclared: true,
    }),
    true,
  );
  for (const [termsAccepted, adulthoodDeclared] of [
    [true, false],
    [false, true],
    [false, false],
  ] as const) {
    assert.equal(
      termsGateAnswersComplete({
        adulthoodDeclarationLabel: label,
        termsAccepted,
        adulthoodDeclared,
      }),
      false,
      `${termsAccepted}/${adulthoodDeclared}`,
    );
  }
});

// The two boxes are two boxes: accepting the Terms is not declaring adulthood,
// and one control cannot say both — declining the Terms means "I do not agree"
// and declining this means "I am a child".
test("ticking the Terms box does not answer the declaration", () => {
  assert.equal(
    termsGateAnswersComplete({
      adulthoodDeclarationLabel: "Declaro ser mayor de edad",
      termsAccepted: true,
      adulthoodDeclared: false,
    }),
    false,
  );
});

// The declaration changes nothing about which navigations are diverted. It has
// no gate of its own — it rides the Terms gate — so the middleware still asks
// exactly one question, and `/api/` stays ungated so a sale in progress at the
// box office commits.
test("the declaration adds no gate of its own to the navigation decision", () => {
  assert.equal(decideTermsGate({ pathname: "/pos", termsOutstanding: false }).kind, "allow");
  assert.equal(decideTermsGate({ pathname: "/api/events/abc/sales", termsOutstanding: true }).kind, "allow");
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
