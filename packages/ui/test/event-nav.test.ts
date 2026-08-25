import assert from "node:assert/strict";
import { test } from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { eventNavItems } from "../src/lib/event-nav.ts";

// Keys rather than labels throughout, the same way staff-nav.test.ts reads: the
// panel's words come from the staff catalog now (ADR 0041), and a copy edit is
// not a change to this module's behaviour.
const litAt = (activePath: string, fullAccess = true, holderList = false) =>
  eventNavItems({ eventId: "evt_1", fullAccess, holderList })
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

// THE DARK DEFAULT. Ticket Assignment and Ticket Questions both ship behind
// flags that are off (ADR 0045), and a nav entry is exactly the kind of thing
// that would admit a feature is there before the Privacy Policy describes it.
// So the entry is absent unless a caller says otherwise — including for a
// caller that has not thought about it, which is what the default argument is
// for. Every test above is a witness to this, since none of them passes the
// flag.
test("the Holder List is absent until it is asked for", () => {
  assert.ok(
    !eventNavItems({ eventId: "evt_1", fullAccess: true })
      .map((item) => item.key)
      .includes("holderList"),
  );
  assert.ok(
    !eventNavItems({ eventId: "evt_1", fullAccess: true, holderList: false })
      .map((item) => item.key)
      .includes("holderList"),
  );
});

// It takes a flag of its OWN rather than riding fullAccess, because its route is
// gated to Org Admins alone — narrower than fullAccess, which also admits an
// Event Owner — and because both features can be dark for everybody. The caller
// establishes both facts; this module only places the entry.
test("the Holder List is offered when asked for, beside Sales", () => {
  assert.deepEqual(
    eventNavItems({ eventId: "evt_1", fullAccess: true, holderList: true }).map(
      (item) => item.key,
    ),
    ["details", "ticketTypes", "reach", "sales", "holderList"],
  );
});

// The href keeps the route's historical name (#333): the screen became the
// Holder List, and its address is where it has always lived — a rename of the
// path would break every bookmark for a word.
test("the Holder List points at the Event's own tab", () => {
  const entry = eventNavItems({
    eventId: "evt_1",
    fullAccess: true,
    holderList: true,
  }).find((item) => item.key === "holderList");
  assert.equal(entry?.href, "/events/evt_1/outstanding-answers");
});

test("the Holder List tab lights on its own page alone", () => {
  assert.deepEqual(litAt("/events/evt_1/outstanding-answers", true, true), ["holderList"]);
});

test("Tags are managed from Details, so they have no entry of their own", () => {
  const keys: string[] = eventNavItems({ eventId: "evt_1", fullAccess: true }).map(
    (item) => item.key,
  );
  assert.ok(!keys.includes("tags"));
});
