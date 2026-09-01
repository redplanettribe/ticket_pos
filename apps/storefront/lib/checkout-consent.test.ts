import assert from "node:assert/strict";
import test from "node:test";

import { anyConsentBox } from "./checkout-consent.ts";

const none = {
  policy_acceptance: false,
  marketing_consent: false,
  networking_consent: false,
  terms_acceptance: false,
  adulthood_declaration: false,
};

test("anyConsentBox is true when any single box is", () => {
  assert.equal(anyConsentBox({ ...none, policy_acceptance: true }), true);
  assert.equal(anyConsentBox({ ...none, marketing_consent: true }), true);
  assert.equal(anyConsentBox({ ...none, networking_consent: true }), true);
  // The Terms box alone draws the section (#537): a Terms edition bump owes
  // the terms box and nothing else, and the section must appear for it.
  assert.equal(anyConsentBox({ ...none, terms_acceptance: true }), true);
  // And the Terms box with the Adulthood Declaration beside it (#588, ADR
  // 0069), which is what a Gating Edition carrying the artifact owes a
  // Customer re-gated mid-session: the pair, drawn together, in the one
  // section.
  assert.equal(
    anyConsentBox({ ...none, terms_acceptance: true, adulthood_declaration: true }),
    true,
  );
  // The declaration alone is a state the API does not produce — it tracks the
  // box above and is never true without it. It draws the section anyway,
  // because the alternative to drawing it is a pay button disabled behind a
  // checkbox nobody can see.
  assert.equal(anyConsentBox({ ...none, adulthood_declaration: true }), true);
});

test("a Customer who owes nothing gets no consent section at all", () => {
  // The ordinary case since ADR 0054: the boxes were met at sign-in, so the
  // dialog draws no notice, no link and no checkbox (parent #249, user story 10).
  assert.equal(anyConsentBox(none), false);
});
