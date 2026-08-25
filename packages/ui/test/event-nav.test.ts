import assert from "node:assert/strict";
import { test } from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { eventNavItems } from "../src/lib/event-nav.ts";

// Keys rather than labels throughout, the same way staff-nav.test.ts reads: the
// panel's words come from the staff catalog now (ADR 0041), and a copy edit is
// not a change to this module's behaviour.
const litAt = (activePath: string, fullAccess = true) =>
  eventNavItems({ eventId: "evt_1", fullAccess })
    .filter((item) => isNavItemActive(activePath, item.href, { exact: item.exact }))
    .map((item) => item.key);

test("Details is the index of the Event, not its owner", () => {
  assert.deepEqual(litAt("/events/evt_1"), ["details"]);
});

test("a page beneath the Event lights its own entry alone", () => {
  assert.deepEqual(litAt("/events/evt_1/sales"), ["sales"]);
  assert.deepEqual(litAt("/events/evt_1/ticket-types"), ["ticketTypes"]);
  assert.deepEqual(litAt("/events/evt_1/reach"), ["reach"]);
});

test("a sibling Event does not light this Event's entries", () => {
  assert.deepEqual(litAt("/events/evt_10"), []);
});

test("Reach stays owner-only; Sales is for every Member", () => {
  assert.deepEqual(
    eventNavItems({ eventId: "evt_1", fullAccess: false }).map((item) => item.key),
    ["details", "ticketTypes", "sales"],
  );
});

// Sales Trends left the panel in #460: it is a sub-tab of the Sales surface now
// (`salesNavItems`), so that the same chart is not offered from two places.
test("Trends has no entry of its own at any access level", () => {
  for (const fullAccess of [true, false]) {
    const keys: string[] = eventNavItems({ eventId: "evt_1", fullAccess }).map((item) => item.key);
    assert.ok(!keys.includes("trends"));
    assert.ok(
      !eventNavItems({ eventId: "evt_1", fullAccess }).some((item) =>
        item.href.endsWith("/trends"),
      ),
    );
  }
});

// Sales owns its subtree, so the panel entry stays lit while a reader is on one
// of the Sales sub-tabs — including the one Trends moved to.
test("Sales is lit on its sub-tabs", () => {
  assert.deepEqual(litAt("/events/evt_1/sales/trends"), ["sales"]);
  assert.deepEqual(litAt("/events/evt_1/sales/record"), ["sales"]);
  assert.deepEqual(litAt("/events/evt_1/sales/holders"), ["sales"]);
});

// Reach owns its subtree the same way (#464): the panel entry stays lit whether
// a reader is on Reach Trends or on the Affiliate Links beneath it. The old
// Affiliate Links address is a redirect now, and lights nothing of its own.
test("Reach is lit on its sub-tabs", () => {
  assert.deepEqual(litAt("/events/evt_1/reach/affiliate-links"), ["reach"]);
  assert.deepEqual(litAt("/events/evt_1/affiliate-links"), []);
});

test("entries read in a fixed order", () => {
  assert.deepEqual(
    eventNavItems({ eventId: "evt_1", fullAccess: true }).map((item) => item.key),
    ["details", "ticketTypes", "reach", "sales"],
  );
});

// The Holder List left the panel in #469: it is the last sub-tab of the Sales
// surface now (`salesNavItems`), so that the same roster is not offered from
// two places. Its old address is a redirect, and lights nothing of its own.
test("the Holder List has no entry of its own at any access level", () => {
  for (const fullAccess of [true, false]) {
    const keys: string[] = eventNavItems({ eventId: "evt_1", fullAccess }).map((item) => item.key);
    assert.ok(!keys.includes("holderList"));
    assert.ok(
      !eventNavItems({ eventId: "evt_1", fullAccess }).some((item) =>
        item.href.endsWith("/outstanding-answers"),
      ),
    );
  }
  assert.deepEqual(litAt("/events/evt_1/outstanding-answers"), []);
});

test("Tags are managed from Details, so they have no entry of their own", () => {
  const keys: string[] = eventNavItems({ eventId: "evt_1", fullAccess: true }).map(
    (item) => item.key,
  );
  assert.ok(!keys.includes("tags"));
});
