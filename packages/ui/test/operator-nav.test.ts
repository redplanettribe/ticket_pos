import assert from "node:assert/strict";
import test from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { operatorNavItems } from "../src/lib/operator-nav.ts";

const labels = (items: Array<{ label: string }>) => items.map((item) => item.label);

// --- what the operator surface offers --------------------------------------

test("the operator sees each job of the dashboard as its own destination, in order", () => {
  assert.deepEqual(labels(operatorNavItems({})), [
    "Overview",
    "Organizations",
    "Payout Requests",
    "Find a sale",
  ]);
});

test("the entries point at the operator surface", () => {
  assert.deepEqual(
    operatorNavItems({}).map((item) => item.href),
    ["/operator", "/operator/organizations", "/operator/payout-requests", "/operator/sales"],
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

test("reading one organization lights Organizations alone", () => {
  assert.deepEqual(activeLabels("/operator/organizations"), ["Organizations"]);
  assert.deepEqual(activeLabels("/operator/organizations/org_123"), ["Organizations"]);
});

test("reading the sale a lookup found lights Find a sale alone", () => {
  assert.deepEqual(activeLabels("/operator/sales"), ["Find a sale"]);
  assert.deepEqual(activeLabels("/operator/sales/TP-J7K2QX9M"), ["Find a sale"]);
});

// --- the pending count -----------------------------------------------------

test("the pending count is worn by Payout Requests and by nothing else", () => {
  const items = operatorNavItems({ payoutRequestBadge: "3" });
  assert.deepEqual(
    items.map((item) => item.badge ?? null),
    [null, null, "3", null],
  );
});

test("nothing waiting means no entry wears anything", () => {
  assert.deepEqual(
    operatorNavItems({ payoutRequestBadge: null }).map((item) => item.badge ?? null),
    [null, null, null, null],
  );
});
