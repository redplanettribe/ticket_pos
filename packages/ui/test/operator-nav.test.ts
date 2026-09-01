import assert from "node:assert/strict";
import test from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";
import { operatorNavItems } from "../src/lib/operator-nav.ts";

/** Keys rather than words — see staff-nav.test.ts for why. */
const keys = (items: Array<{ key: string }>) => items.map((item) => item.key);

// --- what the operator surface offers --------------------------------------

test("the operator sees each job of the dashboard as its own destination, in order", () => {
  assert.deepEqual(keys(operatorNavItems({})), [
    "overview",
    "organizations",
    "payoutRequests",
    "findSale",
    "taxInvoicing",
    "legalCenter",
  ]);
});

test("the entries point at the operator surface", () => {
  assert.deepEqual(
    operatorNavItems({}).map((item) => item.href),
    [
      "/operator",
      "/operator/organizations",
      "/operator/payout-requests",
      "/operator/sales",
      "/operator/invoicing",
      "/operator/legal",
    ],
  );
});

// --- which entry is lit ----------------------------------------------------

const activeKeys = (activePath: string) =>
  keys(operatorNavItems({}).filter((item) => isNavItemActive(activePath, item.href, { exact: item.exact })));

test("Overview is lit on the dashboard itself and nowhere else", () => {
  assert.deepEqual(activeKeys("/operator"), ["overview"]);
});

test("reading a single payout request lights Payout Requests alone", () => {
  assert.deepEqual(activeKeys("/operator/payout-requests"), ["payoutRequests"]);
  assert.deepEqual(activeKeys("/operator/payout-requests/pr_123"), ["payoutRequests"]);
});

test("reading one organization lights Organizations alone", () => {
  assert.deepEqual(activeKeys("/operator/organizations"), ["organizations"]);
  assert.deepEqual(activeKeys("/operator/organizations/org_123"), ["organizations"]);
});

test("reading the sale a lookup found lights Find a sale alone", () => {
  assert.deepEqual(activeKeys("/operator/sales"), ["findSale"]);
  assert.deepEqual(activeKeys("/operator/sales/TP-J7K2QX9M"), ["findSale"]);
});

test("the retired consent surface lights nothing at all", () => {
  // /operator/consent was deleted in #566: the withdrawal it offered folded
  // into the person's own consent record inside the Legal Center, so the path
  // names no page and must light no entry. Asserted rather than merely deleted,
  // because a stale bookmark should land on nothing rather than on a navigation
  // still claiming the surface is there.
  assert.deepEqual(activeKeys("/operator/consent"), []);
});

test("the legal center lights Legal center alone, per-subject records included", () => {
  // It was linked from the navigation on the day it was built, unlike the
  // consent surface it replaced (#271), which sat reachable only by URL.
  assert.deepEqual(activeKeys("/operator/legal"), ["legalCenter"]);
  // The acceptance browsers and the per-subject records hang beneath it, so the
  // one entry stays lit however deep into the Legal Center an operator is.
  assert.deepEqual(activeKeys("/operator/legal/acceptances/customers"), ["legalCenter"]);
  assert.deepEqual(
    activeKeys("/operator/legal/acceptances/customers/8f1c0c66-0000-4000-8000-000000000000"),
    ["legalCenter"],
  );
});

test("the invoicing surface lights Tax invoicing alone", () => {
  // The list at the root of the subtree, the Issuer page and an invoice detail
  // all hang beneath /operator/invoicing, so each lights the one entry.
  assert.deepEqual(activeKeys("/operator/invoicing"), ["taxInvoicing"]);
  assert.deepEqual(activeKeys("/operator/invoicing/issuer"), ["taxInvoicing"]);
  assert.deepEqual(activeKeys("/operator/invoicing/inv_123"), ["taxInvoicing"]);
});

// --- the pending count -----------------------------------------------------

test("the pending count is worn by Payout Requests and by nothing else", () => {
  const items = operatorNavItems({ payoutRequestBadge: "3" });
  assert.deepEqual(
    items.map((item) => item.badge ?? null),
    [null, null, "3", null, null, null],
  );
});

test("nothing waiting means no entry wears anything", () => {
  assert.deepEqual(
    operatorNavItems({ payoutRequestBadge: null }).map((item) => item.badge ?? null),
    [null, null, null, null, null, null],
  );
});
