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
  assert.deepEqual(litAt("/events/evt_1/affiliate-links"), ["affiliateLinks"]);
  assert.deepEqual(litAt("/events/evt_1/trends"), ["trends"]);
});

test("a sibling Event does not light this Event's entries", () => {
  assert.deepEqual(litAt("/events/evt_10"), []);
});

test("Affiliate Links stays owner-only; Sales is for every Member", () => {
  assert.deepEqual(
    eventNavItems({ eventId: "evt_1", fullAccess: false }).map((item) => item.key),
    ["details", "ticketTypes", "sales"],
  );
});

// Sales Trends carries the guard the Event's money already has, so an Event
// Staff member is not offered a tab they would only be refused (#276).
test("Trends is offered on full access and absent otherwise", () => {
  assert.ok(
    eventNavItems({ eventId: "evt_1", fullAccess: true })
      .map((item) => item.key)
      .includes("trends"),
  );
  assert.ok(
    !eventNavItems({ eventId: "evt_1", fullAccess: false })
      .map((item) => item.key)
      .includes("trends"),
  );
});

test("Trends points at the Event's own Trends tab", () => {
  const trends = eventNavItems({ eventId: "evt_1", fullAccess: true }).find(
    (item) => item.key === "trends",
  );
  assert.equal(trends?.href, "/events/evt_1/trends");
});

test("entries read in a fixed order", () => {
  assert.deepEqual(
    eventNavItems({ eventId: "evt_1", fullAccess: true }).map((item) => item.key),
    ["details", "ticketTypes", "affiliateLinks", "sales", "trends"],
  );
});

test("Tags are managed from Details, so they have no entry of their own", () => {
  const keys: string[] = eventNavItems({ eventId: "evt_1", fullAccess: true }).map(
    (item) => item.key,
  );
  assert.ok(!keys.includes("tags"));
});
