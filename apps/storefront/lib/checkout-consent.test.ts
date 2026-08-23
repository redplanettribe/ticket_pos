import assert from "node:assert/strict";
import test from "node:test";

import { anyConsentBox } from "./checkout-consent.ts";

test("anyConsentBox is true when any single box is", () => {
  assert.equal(
    anyConsentBox({ policy_acceptance: true, marketing_consent: false, networking_consent: false }),
    true,
  );
  assert.equal(
    anyConsentBox({ policy_acceptance: false, marketing_consent: true, networking_consent: false }),
    true,
  );
  assert.equal(
    anyConsentBox({ policy_acceptance: false, marketing_consent: false, networking_consent: true }),
    true,
  );
});

test("a Customer who owes nothing gets no consent section at all", () => {
  // The ordinary case since ADR 0054: the boxes were met at sign-in, so the
  // dialog draws no notice, no link and no checkbox (parent #249, user story 10).
  assert.equal(
    anyConsentBox({ policy_acceptance: false, marketing_consent: false, networking_consent: false }),
    false,
  );
});
