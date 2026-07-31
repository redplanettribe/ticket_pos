import assert from "node:assert/strict";
import test from "node:test";

import {
  pendingPayoutRequestBadge,
  switcherEntries,
  type SwitcherEntry,
} from "./organization-switcher.ts";

type Org = { member_id: string; organization_name: string };

const acme: Org = { member_id: "mem_1", organization_name: "Acme" };
const beta: Org = { member_id: "mem_2", organization_name: "Beta" };

const kinds = (entries: SwitcherEntry<Org>[]) => entries.map((entry) => entry.kind);

// --- which organizations -----------------------------------------------------

test("a member of two organizations sees both, in the order given", () => {
  const entries = switcherEntries({ memberships: [acme, beta], isPlatformOperator: false });

  assert.deepEqual(kinds(entries), ["organization", "organization"]);
  assert.deepEqual(
    entries.map((entry) => (entry.kind === "organization" ? entry.membership.organization_name : null)),
    ["Acme", "Beta"],
  );
});

// --- whether Platform appears ------------------------------------------------

test("a Platform Operator sees a Platform entry after their organizations", () => {
  const entries = switcherEntries({ memberships: [acme], isPlatformOperator: true });

  assert.deepEqual(kinds(entries), ["organization", "platform"]);
});

test("a staff user who is not a Platform Operator sees no Platform entry", () => {
  const entries = switcherEntries({ memberships: [acme], isPlatformOperator: false });

  assert.deepEqual(kinds(entries), ["organization"]);
});

test("a Platform Operator who is a member of no Organization still gets one entry", () => {
  const entries = switcherEntries({ memberships: [], isPlatformOperator: true });

  assert.deepEqual(kinds(entries), ["platform"]);
});

test("a non-operator with no Membership gets an empty switcher", () => {
  assert.deepEqual(switcherEntries({ memberships: [], isPlatformOperator: false }), []);
});

// --- the waiting-requests badge ----------------------------------------------

test("a count of waiting requests is shown", () => {
  assert.equal(pendingPayoutRequestBadge(3), 3);
});

test("nothing waiting shows no badge", () => {
  assert.equal(pendingPayoutRequestBadge(0), null);
});

test("a count that could not be read shows no badge", () => {
  assert.equal(pendingPayoutRequestBadge(null), null);
  assert.equal(pendingPayoutRequestBadge(undefined), null);
});
