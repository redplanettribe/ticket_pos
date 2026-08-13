import assert from "node:assert/strict";
import test from "node:test";

import { resolveAuthForkRedirectPath, signedInLandingPath } from "./auth-fork.ts";

/**
 * signedInLandingPath answers one question — "this request carries a valid
 * Staff Session, where does it belong?" — for two callers in middleware.ts: the
 * visitor who asked for a page their session does not fit, and the visitor who
 * asked for /login while already signed in.
 *
 * The second caller is why this exists. A Member who presses "Create an event"
 * on the Storefront (#259) lands on /login holding a perfectly good session,
 * and must be carried onward rather than shown a sign-in form.
 */

const member = { member_id: "m-1" };

test("a Member with an active Organization lands on the dashboard", () => {
  const path = signedInLandingPath({
    active_member: { member_id: "m-1" },
    memberships: [member],
  });

  assert.equal(path, "/");
});

test("a session with no Membership at all is sent to create an Organization", () => {
  const path = signedInLandingPath({ active_member: null, memberships: [] });

  assert.equal(path, "/organizations/new");
});

test("one Membership, not yet active, goes through the picker that selects it", () => {
  const path = signedInLandingPath({ active_member: null, memberships: [member] });

  assert.equal(path, "/select-organization");
});

test("several Memberships and none active is the picker", () => {
  const path = signedInLandingPath({
    active_member: null,
    memberships: [member, { member_id: "m-2" }],
  });

  assert.equal(path, "/select-organization");
});

// --- the operator exception (ADR 0015) ------------------------------------

test("a pure operator goes to the operator dashboard, not the create-org dead end", () => {
  const path = signedInLandingPath({
    active_member: null,
    memberships: [],
    is_platform_operator: true,
  });

  // Without the exception this is /organizations/new, which an operator holding
  // no Membership has no reason to be offered.
  assert.equal(path, "/operator");
});

test("an operator who is also an active Member is treated as the Member", () => {
  const path = signedInLandingPath({
    active_member: { member_id: "m-1" },
    memberships: [member],
    is_platform_operator: true,
  });

  assert.equal(path, "/");
});

// --- the contract with the fork it wraps -----------------------------------

test("a non-operator session lands wherever the plain fork says", () => {
  for (const session of [
    { active_member: null, memberships: [] },
    { active_member: null, memberships: [member] },
    { active_member: { member_id: "m-1" }, memberships: [member] },
  ]) {
    assert.equal(signedInLandingPath(session), resolveAuthForkRedirectPath(session));
  }
});

test("every landing is an absolute in-app path", () => {
  for (const session of [
    { active_member: null, memberships: [] },
    { active_member: null, memberships: [member] },
    { active_member: null, memberships: [], is_platform_operator: true },
    { active_member: { member_id: "m-1" }, memberships: [member] },
  ]) {
    const path = signedInLandingPath(session);
    // Never /login: that is what this function exists to route people away from,
    // and a landing of /login would be an infinite redirect.
    assert.notEqual(path, "/login");
    assert.match(path, /^\/[a-z-]*(\/[a-z-]+)*$/);
  }
});
