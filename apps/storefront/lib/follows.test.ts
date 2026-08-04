/**
 * The Follows helpers every surface with a Follow control on it reads through
 * (#218).
 *
 * These tests exist for one failure this app cannot otherwise catch. The Follows
 * list is a discriminated union arriving from one endpoint carrying two kinds,
 * and the mistake that matters is narrowing on the wrong field — asking a Tag
 * entry for `organization.slug`, or matching a Tag's canonical key against an
 * Organization's slug. TypeScript catches the first; nothing but a test catches
 * the second, because both are strings and both are usually different.
 *
 * The API's own rules — idempotency, session scope, what is followable — are the
 * backend's and are covered at the HTTP seam in
 * `backend/integration/customer_follows_test.go`. Nothing here re-states them.
 */

import assert from "node:assert/strict";
import test from "node:test";

import type { Follows, SessionOutcome } from "./customer-session.ts";
// The ".ts" is written out because these tests run the modules directly under
// `node --experimental-strip-types`, which resolves specifiers exactly.
import {
  followList,
  followsOrganization,
  followsTag,
  organizationFollowEndpoint,
  tagFollowEndpoint,
} from "./follows.ts";

/** A listing carrying both kinds, interleaved as the API returns them. */
const bothKinds: SessionOutcome<Follows> = {
  status: "ok",
  data: {
    follows: [
      {
        type: "tag",
        followed_at: "2026-03-02T10:00:00Z",
        tag: { canonical_key: "warehouse techno", name: "Warehouse Techno", curated: false },
      },
      {
        type: "organization",
        followed_at: "2026-03-01T10:00:00Z",
        organization: { name: "Test Org", slug: "test-org", logo_url: null },
      },
      {
        type: "tag",
        followed_at: "2026-02-28T10:00:00Z",
        tag: { canonical_key: "arts & theatre", name: "Arts & Theatre", curated: true },
      },
    ],
  },
};

test("a Tag Follow is not mistaken for an Organization Follow, or the reverse", () => {
  assert.equal(followsTag(bothKinds, "warehouse techno"), true);
  assert.equal(followsOrganization(bothKinds, "test-org"), true);

  // The crossed pair: each identifier looked up under the other kind. Both are
  // plain strings, so only the discriminator keeps these apart.
  assert.equal(followsTag(bothKinds, "test-org"), false);
  assert.equal(followsOrganization(bothKinds, "warehouse techno"), false);
});

test("something not followed is not followed", () => {
  assert.equal(followsTag(bothKinds, "comedy"), false);
  assert.equal(followsOrganization(bothKinds, "other-org"), false);
});

test("a Custom Tag is followable and reported like any other", () => {
  // ADR 0030: the followable pool is deliberately not narrowed to curated Tags.
  const custom = bothKinds.status === "ok" ? bothKinds.data.follows[0] : null;
  assert.equal(custom?.type, "tag");
  assert.equal(custom?.type === "tag" ? custom.tag.curated : true, false);
  assert.equal(followsTag(bothKinds, "warehouse techno"), true);
});

test("a signed-out or failed read follows nothing and lists nothing", () => {
  // Not an error and not a crash: a page that could not read the Follows draws
  // no control, and these answer for that case rather than making every caller
  // guard it. "No" and "cannot say" are the same answer here on purpose.
  for (const outcome of [
    { status: "signed-out" } as const,
    { status: "error", code: "BOOM", message: "boom" } as const,
  ] satisfies SessionOutcome<Follows>[]) {
    assert.equal(followsTag(outcome, "warehouse techno"), false);
    assert.equal(followsOrganization(outcome, "test-org"), false);
    assert.deepEqual(followList(outcome), []);
  }
});

test("the list is handed back in the order the API returned it", () => {
  // One list, one order, both kinds interleaved. Re-sorting in the browser is
  // how two surfaces come to disagree about what "most recent" means.
  assert.deepEqual(
    followList(bothKinds).map((follow) =>
      follow.type === "tag" ? `tag:${follow.tag.canonical_key}` : `organization:${follow.organization.slug}`,
    ),
    ["tag:warehouse techno", "organization:test-org", "tag:arts & theatre"],
  );
});

test("a canonical key that is not URL-safe survives the address", () => {
  // "arts & theatre" is a seeded Preset Tag, not an exotic case: unencoded, the
  // ampersand would end the path and the route would receive "arts ".
  assert.equal(
    tagFollowEndpoint("arts & theatre"),
    "/api/customer/follows/tags/arts%20%26%20theatre",
  );
  assert.equal(tagFollowEndpoint("music"), "/api/customer/follows/tags/music");
});

test("an Organization is addressed by slug", () => {
  assert.equal(
    organizationFollowEndpoint("test-org"),
    "/api/customer/follows/organizations/test-org",
  );
});
