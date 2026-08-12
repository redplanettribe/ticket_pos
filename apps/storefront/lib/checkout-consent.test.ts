import assert from "node:assert/strict";
import test from "node:test";

import {
  anyConsentBox,
  checkoutConsentBoxes,
  EVERY_CONSENT_BOX,
  type ConsentBoxes,
} from "./checkout-consent.ts";

/** A session for `email` owing exactly the boxes named. */
function session(email: string, boxes: Partial<ConsentBoxes>) {
  return {
    email,
    consent_boxes: {
      policy_acceptance: false,
      marketing_consent: false,
      networking_consent: false,
      ...boxes,
    },
  };
}

test("a guest is shown every box", () => {
  assert.deepEqual(checkoutConsentBoxes(null, "ana@example.com"), EVERY_CONSENT_BOX);
  // Including before anything has been typed at all.
  assert.deepEqual(checkoutConsentBoxes(null, ""), EVERY_CONSENT_BOX);
});

test("a fully answered Customer is shown nothing", () => {
  const boxes = checkoutConsentBoxes(session("ana@example.com", {}), "ana@example.com");
  assert.deepEqual(boxes, {
    policy_acceptance: false,
    marketing_consent: false,
    networking_consent: false,
  });
  assert.equal(anyConsentBox(boxes), false);
});

test("a Customer with no Policy Acceptance is shown the required box here too", () => {
  // The legacy Customer, and the one caught by a Policy Version bump: the API
  // says the required box is outstanding and this app draws it, at checkout as
  // much as at sign-in.
  const boxes = checkoutConsentBoxes(
    session("ana@example.com", { policy_acceptance: true }),
    "ana@example.com",
  );
  assert.deepEqual(boxes, {
    policy_acceptance: true,
    marketing_consent: false,
    networking_consent: false,
  });
  assert.equal(anyConsentBox(boxes), true);
});

test("only the unanswered optional boxes appear, without the required one", () => {
  assert.deepEqual(
    checkoutConsentBoxes(session("ana@example.com", { marketing_consent: true }), "ana@example.com"),
    { policy_acceptance: false, marketing_consent: true, networking_consent: false },
  );
});

test("the address is compared without case or surrounding space", () => {
  const signedIn = session("ana@example.com", {});
  assert.deepEqual(checkoutConsentBoxes(signedIn, "  ANA@Example.com "), signedIn.consent_boxes);
});

test("typing somebody else's address turns the dialog back into a guest checkout", () => {
  // The friend's consent is nobody's to have answered, and the API will treat
  // this checkout as a guest's — so every box comes back, including the required
  // one that gates the purchase.
  assert.deepEqual(
    checkoutConsentBoxes(session("ana@example.com", {}), "friend@example.com"),
    EVERY_CONSENT_BOX,
  );
});

test("anyConsentBox is true when any single box is", () => {
  assert.equal(anyConsentBox(EVERY_CONSENT_BOX), true);
  assert.equal(
    anyConsentBox({ policy_acceptance: false, marketing_consent: false, networking_consent: true }),
    true,
  );
  assert.equal(
    anyConsentBox({ policy_acceptance: false, marketing_consent: false, networking_consent: false }),
    false,
  );
});
