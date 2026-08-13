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
  assert.deepEqual(litAt("/events/evt_1/trends"), ["Trends"]);
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

// Sales Trends carries the guard the Event's money already has, so an Event
// Staff member is not offered a tab they would only be refused (#276).
test("Trends is offered on full access and absent otherwise", () => {
  assert.ok(
    eventNavItems({ eventId: "evt_1", fullAccess: true })
      .map((item) => item.label)
      .includes("Trends"),
  );
  assert.ok(
    !eventNavItems({ eventId: "evt_1", fullAccess: false })
      .map((item) => item.label)
      .includes("Trends"),
  );
});

test("Trends points at the Event's own Trends tab", () => {
  const trends = eventNavItems({ eventId: "evt_1", fullAccess: true }).find(
    (item) => item.label === "Trends",
  );
  assert.equal(trends?.href, "/events/evt_1/trends");
});

test("entries read in a fixed order", () => {
  assert.deepEqual(
    eventNavItems({ eventId: "evt_1", fullAccess: true }).map((item) => item.label),
    ["Details", "Ticket Types", "Affiliate Links", "Sales", "Trends"],
  );
});

test("Tags are managed from Details, so they have no entry of their own", () => {
  const labels = eventNavItems({ eventId: "evt_1", fullAccess: true }).map((item) => item.label);
  assert.ok(!labels.includes("Tags"));
});
