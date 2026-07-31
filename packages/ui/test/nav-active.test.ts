import assert from "node:assert/strict";
import test from "node:test";

import { isNavItemActive } from "../src/lib/nav-active.ts";

// --- exact matches ---------------------------------------------------------

test("an entry is active on its own path", () => {
  assert.equal(isNavItemActive("/payouts", "/payouts"), true);
});

test("the root entry is active only on the root path", () => {
  assert.equal(isNavItemActive("/", "/"), true);
  assert.equal(isNavItemActive("/events", "/"), false);
});

// --- descendant matches ----------------------------------------------------

test("an entry is active for a path beneath it", () => {
  assert.equal(isNavItemActive("/operator/payout-requests", "/operator"), true);
  assert.equal(isNavItemActive("/operator/payout-requests/pr_123", "/operator"), true);
});

// --- the index entry of a surface --------------------------------------------

test("an exact entry is active only on its own path", () => {
  assert.equal(isNavItemActive("/operator", "/operator", { exact: true }), true);
  assert.equal(isNavItemActive("/operator/payout-requests", "/operator", { exact: true }), false);
  assert.equal(isNavItemActive("/operator/payout-requests/pr_123", "/operator", { exact: true }), false);
});

// --- shared text prefix, different path --------------------------------------

test("a sibling sharing a text prefix does not activate the entry", () => {
  assert.equal(isNavItemActive("/payouts-archive", "/payouts"), false);
});

test("a path above the entry does not activate it", () => {
  assert.equal(isNavItemActive("/operator", "/operator/payout-requests"), false);
});

// --- no current path -------------------------------------------------------

test("no active path activates nothing", () => {
  assert.equal(isNavItemActive(undefined, "/operator"), false);
});
