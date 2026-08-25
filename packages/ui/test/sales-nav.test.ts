import assert from "node:assert/strict";
import { test } from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { salesNavItems, shouldDrawTabStrip } from "../src/lib/sales-nav.ts";

// Keys rather than labels throughout, the same way event-nav.test.ts reads: the
// strip's words come from the staff catalog (ADR 0041), and a copy edit is not a
// change to this module's behaviour.
const keysFor = (fullAccess: boolean) =>
  salesNavItems({ eventId: "evt_1", fullAccess }).map((item) => item.key);

const litAt = (activePath: string, fullAccess = true) =>
  salesNavItems({ eventId: "evt_1", fullAccess })
    .filter((item) => isNavItemActive(activePath, item.href, { exact: item.exact }))
    .map((item) => item.key);

test("the tabs read in a fixed order on full access", () => {
  assert.deepEqual(keysFor(true), ["sales", "record", "trends"]);
});

test("Record and Trends are owner-only, so Event Staff are offered the list alone", () => {
  assert.deepEqual(keysFor(false), ["sales"]);
});

test("each tab points at its own address", () => {
  const hrefs = Object.fromEntries(
    salesNavItems({ eventId: "evt_1", fullAccess: true }).map((item) => [item.key, item.href]),
  );
  assert.deepEqual(hrefs, {
    sales: "/events/evt_1/sales",
    record: "/events/evt_1/sales/record",
    trends: "/events/evt_1/sales/trends",
  });
});

// Sales is the index of its own surface: without `exact` it would stay lit on
// every sub-tab beneath it, and a reader on Record would see two tabs claiming
// to be the current page.
test("Sales is lit only at its own path", () => {
  assert.deepEqual(litAt("/events/evt_1/sales"), ["sales"]);
  assert.deepEqual(litAt("/events/evt_1/sales/record"), ["record"]);
});

// An Event Staff member is offered the list alone, so the tab they were sent to
// lights nothing at all — the state they are in for the instant before the
// route's redirect carries them back to the list.
test("a tab that is not offered lights nothing", () => {
  assert.deepEqual(litAt("/events/evt_1/sales/record", false), []);
  assert.deepEqual(litAt("/events/evt_1/sales/trends", false), []);
});

// Sales Trends came here from the Event panel in #460, and it carries the gate
// it always had: it reads the Event's money, so it is for the Org Admin and the
// Event Owner and is hidden from Event Staff rather than shown and refused.
test("Trends is offered on full access and absent otherwise", () => {
  assert.ok(keysFor(true).includes("trends"));
  assert.ok(!keysFor(false).includes("trends"));
});

test("Trends lights its own entry alone", () => {
  assert.deepEqual(litAt("/events/evt_1/sales/trends"), ["trends"]);
});

test("a sibling Event lights nothing", () => {
  assert.deepEqual(litAt("/events/evt_10/sales"), []);
  assert.deepEqual(litAt("/events/evt_10/sales/record"), []);
  assert.deepEqual(litAt("/events/evt_10/sales/trends"), []);
});

// A strip with one tab in it says nothing a reader did not already know, so
// Event Staff get the list with no strip above it at all.
test("one entry means no strip", () => {
  assert.equal(shouldDrawTabStrip(salesNavItems({ eventId: "evt_1", fullAccess: true })), true);
  assert.equal(shouldDrawTabStrip(salesNavItems({ eventId: "evt_1", fullAccess: false })), false);
  assert.equal(shouldDrawTabStrip([]), false);
});
