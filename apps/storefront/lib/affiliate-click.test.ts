import assert from "node:assert/strict";
import test from "node:test";

import { affiliateCodeFromRef } from "./affiliate-click.ts";
import { readAffiliateCodes, rememberAffiliateClick } from "./affiliate-ref.ts";

const CLICKED_AT = Date.parse("2026-07-01T12:00:00Z");

test("an Event page reached through an Affiliate Link carries its code", () => {
  assert.equal(affiliateCodeFromRef("K7QM2XZ4"), "K7QM2XZ4");
});

test("an Event page reached without a ref carries no Affiliate Link code", () => {
  assert.equal(affiliateCodeFromRef(undefined), null);
  assert.equal(affiliateCodeFromRef(""), null);
  assert.equal(affiliateCodeFromRef("   "), null);
});

// A pasted link can arrive with whitespace around the code; a dead code is the
// endpoint's problem, an unsendable one is ours.
test("a ref surrounded by whitespace still carries its code", () => {
  assert.equal(affiliateCodeFromRef("  K7QM2XZ4 "), "K7QM2XZ4");
});

// ?ref=A&ref=B is legal in a URL and Next hands it over as an array. The first
// one is the link that was followed.
test("a repeated ref carries the first code", () => {
  assert.equal(affiliateCodeFromRef(["K7QM2XZ4", "OTHER123"]), "K7QM2XZ4");
  assert.equal(affiliateCodeFromRef([]), null);
});

// Anyone can put anything in a query string. A ref far longer than a code is
// junk, not a mistyped code, and is dropped before it becomes a request.
test("a ref too long to be an Affiliate Link code is dropped", () => {
  assert.equal(affiliateCodeFromRef("X".repeat(65)), null);
  assert.equal(affiliateCodeFromRef("not a code"), null);
});

// The click counter and the attribution cookie must agree about what the code
// IS, or a pasted lowercase ref attributes a sale it never counted a click for.
// One normalizer, applied on both paths.
test("a lowercase ref is normalized to the code the Affiliate Link issued", () => {
  assert.equal(affiliateCodeFromRef("k7qm2xz4"), "K7QM2XZ4");
  assert.equal(
    affiliateCodeFromRef(" k7qm2xz4 "),
    readAffiliateCodes(
      rememberAffiliateClick(null, "demo-venue", "rave", " k7qm2xz4 ", CLICKED_AT),
      "demo-venue",
      "rave",
      CLICKED_AT,
    )[0],
  );
});
