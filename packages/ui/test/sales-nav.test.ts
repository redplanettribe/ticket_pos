import assert from "node:assert/strict";
import { test } from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { salesNavItems, shouldDrawTabStrip } from "../src/lib/sales-nav.ts";

// Keys rather than labels throughout, the same way event-nav.test.ts reads: the
// strip's words come from the staff catalog (ADR 0041), and a copy edit is not a
// change to this module's behaviour.
const keysFor = (fullAccess: boolean, holderList = false) =>
  salesNavItems({ eventId: "evt_1", fullAccess, holderList }).map((item) => item.key);

const litAt = (activePath: string, fullAccess = true, holderList = false) =>
  salesNavItems({ eventId: "evt_1", fullAccess, holderList })
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
    salesNavItems({ eventId: "evt_1", fullAccess: true, holderList: true }).map((item) => [
      item.key,
      item.href,
    ]),
  );
  assert.deepEqual(hrefs, {
    sales: "/events/evt_1/sales",
    record: "/events/evt_1/sales/record",
    trends: "/events/evt_1/sales/trends",
    holderList: "/events/evt_1/sales/holders",
  });
});

// Sales is the index of its own surface: without `exact` it would stay lit on
// every sub-tab beneath it, and a reader on Record would see two tabs claiming
// to be the current page.
test("Sales is lit only at its own path", () => {
  assert.deepEqual(litAt("/events/evt_1/sales"), ["sales"]);
  assert.deepEqual(litAt("/events/evt_1/sales/record"), ["record"]);
  assert.deepEqual(litAt("/events/evt_1/sales/holders", true, true), ["holderList"]);
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

// The Holder List came here from the Event panel in #469, and it keeps the gate
// it always had — which is not the surface's own. THE DARK DEFAULT first:
// Ticket Assignment and Ticket Questions both ship behind flags that are off
// (ADR 0045), and a nav entry is exactly the kind of thing that would admit a
// feature is there before the Privacy Policy describes it. So the tab is absent
// unless a caller says otherwise — including for a caller that has not thought
// about it, which is what the default argument is for. Every test above is a
// witness to this, since none of them passes the flag.
test("the Holder List is absent until it is asked for", () => {
  assert.ok(!keysFor(true).includes("holderList"));
  assert.ok(!keysFor(true, false).includes("holderList"));
});

// It takes a flag of its OWN rather than riding fullAccess, and since #521 for
// one reason rather than two: both features can be dark for everybody, so an
// Org Admin on an Event with neither is offered three tabs and not four. Its
// AUDIENCE is no longer the narrow half of that — ADR 0065 opened the read to
// the Event Owner, so the caller now passes fullAccess && the flag rather than
// checking for an Org Admin first. The caller establishes it either way; this
// module only places the entry, and places it last: after every reading of what
// was sold, who is coming on it.
test("the Holder List is offered last when asked for", () => {
  assert.deepEqual(keysFor(true, true), ["sales", "record", "trends", "holderList"]);
});

test("the Holder List lights its own entry alone", () => {
  assert.deepEqual(litAt("/events/evt_1/sales/holders", true, true), ["holderList"]);
  assert.deepEqual(litAt("/events/evt_1/sales", true, true), ["sales"]);
});

// The tab somebody was sent to without the gate lights nothing — the instant
// before the route's redirect carries them back to the list.
test("the Holder List tab that is not offered lights nothing", () => {
  assert.deepEqual(litAt("/events/evt_1/sales/holders", true, false), []);
  assert.deepEqual(litAt("/events/evt_1/sales/holders", false, false), []);
});

test("a sibling Event lights nothing", () => {
  assert.deepEqual(litAt("/events/evt_10/sales"), []);
  assert.deepEqual(litAt("/events/evt_10/sales/record"), []);
  assert.deepEqual(litAt("/events/evt_10/sales/trends"), []);
  assert.deepEqual(litAt("/events/evt_10/sales/holders", true, true), []);
});

// A strip with one tab in it says nothing a reader did not already know, so
// Event Staff get the list with no strip above it at all.
test("one entry means no strip", () => {
  assert.equal(shouldDrawTabStrip(salesNavItems({ eventId: "evt_1", fullAccess: true })), true);
  assert.equal(shouldDrawTabStrip(salesNavItems({ eventId: "evt_1", fullAccess: false })), false);
  assert.equal(shouldDrawTabStrip([]), false);
});
