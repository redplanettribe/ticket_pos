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
    "customerConsent",
    "taxInvoicing",
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
      "/operator/consent",
      "/operator/invoicing",
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

test("the consent surface lights Customer consent alone", () => {
  assert.deepEqual(activeKeys("/operator/consent"), ["customerConsent"]);
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
