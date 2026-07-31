import assert from "node:assert/strict";
import test from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { operatorNavItems } from "../src/lib/operator-nav.ts";

const labels = (items: Array<{ label: string }>) => items.map((item) => item.label);

// --- what the operator surface offers --------------------------------------

test("the operator sees Overview and Payout Requests, in that order", () => {
  assert.deepEqual(labels(operatorNavItems({})), ["Overview", "Payout Requests"]);
});

test("the entries point at the operator surface", () => {
  assert.deepEqual(
    operatorNavItems({}).map((item) => item.href),
    ["/operator", "/operator/payout-requests"],
  );
});

// --- which entry is lit ----------------------------------------------------

const activeLabels = (activePath: string) =>
  labels(operatorNavItems({}).filter((item) => isNavItemActive(activePath, item.href, { exact: item.exact })));

test("Overview is lit on the dashboard itself and nowhere else", () => {
  assert.deepEqual(activeLabels("/operator"), ["Overview"]);
});

test("reading a single payout request lights Payout Requests alone", () => {
  assert.deepEqual(activeLabels("/operator/payout-requests"), ["Payout Requests"]);
  assert.deepEqual(activeLabels("/operator/payout-requests/pr_123"), ["Payout Requests"]);
});

// --- the pending count -----------------------------------------------------

test("the pending count is worn by Payout Requests and by nothing else", () => {
  const items = operatorNavItems({ payoutRequestBadge: "3" });
  assert.deepEqual(
    items.map((item) => item.badge ?? null),
    [null, "3"],
  );
});

test("nothing waiting means no entry wears anything", () => {
  assert.deepEqual(
    operatorNavItems({ payoutRequestBadge: null }).map((item) => item.badge ?? null),
    [null, null],
  );
});
