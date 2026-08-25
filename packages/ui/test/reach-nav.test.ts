import assert from "node:assert/strict";
import { test } from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { reachNavItems } from "../src/lib/reach-nav.ts";

// Keys rather than labels throughout, the same way sales-nav.test.ts reads: the
// strip's words come from the staff catalog (ADR 0041), and a copy edit is not a
// change to this module's behaviour.
const keys = () => reachNavItems({ eventId: "evt_1" }).map((item) => item.key);

const litAt = (activePath: string) =>
  reachNavItems({ eventId: "evt_1" })
    .filter((item) => isNavItemActive(activePath, item.href, { exact: item.exact }))
    .map((item) => item.key);

// Trends first: the surface is about how the page was reached, and the links are
// one part of that — the reverse of the order the old Affiliate Links tab kept.
test("the tabs read Trends then Affiliate Links", () => {
  assert.deepEqual(keys(), ["trends", "affiliateLinks"]);
});

test("each tab points at its own address", () => {
  const hrefs = Object.fromEntries(
    reachNavItems({ eventId: "evt_1" }).map((item) => [item.key, item.href]),
  );
  assert.deepEqual(hrefs, {
    trends: "/events/evt_1/reach",
    affiliateLinks: "/events/evt_1/reach/affiliate-links",
  });
});

// Trends is the index of the Reach surface, not its owner: without `exact` it
// would stay lit on the Affiliate Links sub-tab too, and a reader there would
// see two tabs claiming to be the current page.
test("Trends is lit only at its own path", () => {
  assert.deepEqual(litAt("/events/evt_1/reach"), ["trends"]);
});

test("Affiliate Links lights its own entry alone", () => {
  assert.deepEqual(litAt("/events/evt_1/reach/affiliate-links"), ["affiliateLinks"]);
});

test("a sibling Event lights nothing", () => {
  assert.deepEqual(litAt("/events/evt_10/reach"), []);
  assert.deepEqual(litAt("/events/evt_10/reach/affiliate-links"), []);
});
