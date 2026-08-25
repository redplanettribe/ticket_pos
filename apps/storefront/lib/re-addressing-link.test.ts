import assert from "node:assert/strict";
import test from "node:test";

import {
  reAddressingLinkFailure,
  reAddressingLinkNext,
  reAddressingLinkState,
  reAddressingLinkToken,
} from "./re-addressing-link.ts";
import enMessages from "../messages/en.json" with { type: "json" };
import esMessages from "../messages/es.json" with { type: "json" };

// The Re-addressing Link page's rules (#422, ADR 0058).

const view = {
  event_name: "Noche de jazz",
  confirmation_ref: "TP-7Q2KD",
  corrected_email: "carla@example.com",
  accepted_at: null,
};

test("the token is the one string the address carries, trimmed", () => {
  assert.equal(reAddressingLinkToken("abc.def"), "abc.def");
  assert.equal(reAddressingLinkToken("  abc.def\n"), "abc.def");
  // A link that arrived without its token, or with only whitespace, has
  // nothing to send the API.
  assert.equal(reAddressingLinkToken(undefined), "");
  assert.equal(reAddressingLinkToken("   "), "");
  // A repeated parameter is not a link this platform ever wrote; taking the
  // first would be guessing at which one somebody meant.
  assert.equal(reAddressingLinkToken(["abc", "def"]), "");
});

test("a genuine link that has ended says why, because the reader is the buyer", () => {
  // Unlike the Assignment Link, whose reader must not learn what a stranger
  // decided, this page's reader is told their own facts apart.
  const ended = "RE_ADDRESSING_LINK_NO_LONGER_VALID";
  assert.equal(reAddressingLinkFailure(ended, { reason: "withdrawn" }), "withdrawn");
  assert.equal(reAddressingLinkFailure(ended, { reason: "sale_reversed" }), "saleReversed");
  assert.equal(reAddressingLinkFailure(ended, { reason: "event_started" }), "eventStarted");
});

test("a reason this app has not heard of falls through to the honest floor", () => {
  const ended = "RE_ADDRESSING_LINK_NO_LONGER_VALID";
  assert.equal(reAddressingLinkFailure(ended, { reason: "something_new" }), "invalid");
  assert.equal(reAddressingLinkFailure(ended, {}), "invalid");
  assert.equal(reAddressingLinkFailure(ended, undefined), "invalid");
  assert.equal(reAddressingLinkFailure(ended, "withdrawn"), "invalid");
});

test("a forgery, a replaced link and the deployment fault are told apart", () => {
  // Tampered, truncated, cross-purpose and superseded are deliberately one
  // code on the API's side, and one sentence here.
  assert.equal(reAddressingLinkFailure("RE_ADDRESSING_LINK_INVALID"), "invalid");
  // A missing signing key is the platform's fault and says "try again later".
  assert.equal(reAddressingLinkFailure("RE_ADDRESSING_LINK_UNAVAILABLE"), "unavailable");
  assert.equal(reAddressingLinkFailure("SOMETHING_NEW"), "invalid");
  assert.equal(reAddressingLinkFailure(undefined), "invalid");
});

test("the page's state follows the token first and the view read second", () => {
  // No token: nothing to send, so no read is made and the read is not consulted.
  assert.deepEqual(reAddressingLinkState("", null), { kind: "incomplete" });
  assert.deepEqual(reAddressingLinkState("", { status: "ok", view }), { kind: "incomplete" });

  // A token and a view: the button is offered, and an accepted link says so
  // first rather than refusing — a mail opened twice lands on the Sale.
  assert.deepEqual(reAddressingLinkState("tok", { status: "ok", view }), { kind: "ready", view });
  const accepted = { ...view, accepted_at: "2026-08-24T15:00:00Z" };
  assert.deepEqual(reAddressingLinkState("tok", { status: "ok", view: accepted }), {
    kind: "accepted",
    view: accepted,
  });

  // A refusal carries its reason through to the copy key.
  assert.deepEqual(
    reAddressingLinkState("tok", {
      status: "error",
      code: "RE_ADDRESSING_LINK_NO_LONGER_VALID",
      details: { reason: "sale_reversed" },
    }),
    { kind: "failed", failure: "saleReversed" },
  );
  assert.deepEqual(reAddressingLinkState("tok", { status: "error", code: "RE_ADDRESSING_LINK_INVALID" }), {
    kind: "failed",
    failure: "invalid",
  });
  // A read that never reached the API is a refusal with no code: the floor.
  assert.deepEqual(reAddressingLinkState("tok", null), { kind: "failed", failure: "invalid" });
});

test("a press with a session lands on the Sale in the Customer Area", () => {
  // The same anchor the Confirmation Link and checkout success use, so the
  // reader lands on the purchase and not merely on the list.
  assert.equal(
    reAddressingLinkNext({ ticket_sale_id: "sale-1", consent_required: false }),
    "/tickets#sale-sale-1",
  );
});

test("a press with consent outstanding goes to the sign-in page's consent step, bound for the Sale", () => {
  // Exactly where the Google callback sends a first-time Customer: one consent
  // step, one submission endpoint, and the sign-in page pushes them on.
  const href = reAddressingLinkNext({ ticket_sale_id: "sale-1", consent_required: true });
  const url = new URL(href, "https://storefront.test");
  assert.equal(url.pathname, "/signin");
  assert.equal(url.searchParams.get("consent"), "pending");
  assert.equal(url.searchParams.get("next"), "/tickets#sale-sale-1");
});

test("the accepted page's disclosure does not promise that signing in needs nothing else", () => {
  // A Customer who still owes consent is sent to the sign-in page's consent
  // step first (#437); the copy says the Privacy Policy may come first and
  // never claims there is no further step.
  for (const [messages, terms, bare] of [
    [enMessages, "privacy policy", "nothing else"],
    [esMessages, "política de privacidad", "nada más"],
  ] as const) {
    const sentence = messages.reAddressingLink.acceptedDisclosure.toLowerCase();
    assert.equal(sentence.includes(terms), true, `acceptedDisclosure does not mention the ${terms}`);
    assert.equal(sentence.includes(bare), false, `acceptedDisclosure still promises "${bare}"`);
  }
});

test("every failure key the mapper can return has copy in both languages", () => {
  // The mapper returns a key and the page renders `${key}Description`; a key
  // with no sentence behind it is a blank alert for somebody who paid.
  for (const messages of [enMessages, esMessages]) {
    const copy = messages.reAddressingLink as Record<string, string>;
    for (const key of ["invalid", "unavailable", "withdrawn", "saleReversed", "eventStarted"]) {
      assert.equal(typeof copy[`${key}Description`], "string", `${key}Description is missing`);
      assert.notEqual(copy[`${key}Description`].trim(), "");
    }
  }
});

test("the barred words never appear in the page's copy", () => {
  // CONTEXT.md bars "claim" and "transfer" for this act; the verb is accept.
  for (const messages of [enMessages, esMessages]) {
    for (const [key, sentence] of Object.entries(messages.reAddressingLink)) {
      for (const barred of ["claim", "transfer", "reclam", "transfer"]) {
        assert.equal(
          sentence.toLowerCase().includes(barred),
          false,
          `${key} contains "${barred}"`,
        );
      }
    }
  }
});
