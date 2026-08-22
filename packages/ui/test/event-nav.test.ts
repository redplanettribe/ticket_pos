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
    ["details", "ticketTypes", "affiliateLinks", "sales", "holderList", "trends"],
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
