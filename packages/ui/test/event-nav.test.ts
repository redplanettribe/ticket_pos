import assert from "node:assert/strict";
import { test } from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { eventNavItems } from "../src/lib/event-nav.ts";

const litAt = (activePath: string, fullAccess = true) =>
  eventNavItems({ eventId: "evt_1", fullAccess })
    .filter((item) => isNavItemActive(activePath, item.href, { exact: item.exact }))
    .map((item) => item.label);

test("Details is the index of the Event, not its owner", () => {
  assert.deepEqual(litAt("/events/evt_1"), ["Details"]);
});

test("a page beneath the Event lights its own entry alone", () => {
  assert.deepEqual(litAt("/events/evt_1/sales"), ["Sales"]);
  assert.deepEqual(litAt("/events/evt_1/ticket-types"), ["Ticket Types"]);
  assert.deepEqual(litAt("/events/evt_1/affiliate-links"), ["Affiliate Links"]);
});

test("a sibling Event does not light this Event's entries", () => {
  assert.deepEqual(litAt("/events/evt_10"), []);
});

test("Affiliate Links stays owner-only; Sales is for every Member", () => {
  assert.deepEqual(
    eventNavItems({ eventId: "evt_1", fullAccess: false }).map((item) => item.label),
    ["Details", "Ticket Types", "Sales"],
  );
});

test("entries read in a fixed order", () => {
  assert.deepEqual(
    eventNavItems({ eventId: "evt_1", fullAccess: true }).map((item) => item.label),
    ["Details", "Ticket Types", "Affiliate Links", "Sales"],
  );
});

test("Tags are managed from Details, so they have no entry of their own", () => {
  const labels = eventNavItems({ eventId: "evt_1", fullAccess: true }).map((item) => item.label);
  assert.ok(!labels.includes("Tags"));
});
