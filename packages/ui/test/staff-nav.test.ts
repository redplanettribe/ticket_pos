import assert from "node:assert/strict";
import test from "node:test";

import { staffNavItems } from "../src/lib/staff-nav.ts";

const labels = (items: Array<{ label: string }>) => items.map((item) => item.label);

// --- what everybody sees ---------------------------------------------------

test("a session with no organization sees only the unconditional entries", () => {
  assert.deepEqual(labels(staffNavItems({})), ["Dashboard", "POS"]);
});

// --- the gated entries -----------------------------------------------------

test("Events appears for a member of an organization", () => {
  assert.deepEqual(labels(staffNavItems({ showEvents: true })), ["Dashboard", "Events", "POS"]);
});

test("an Org Admin sees Payouts between POS and Settings", () => {
  assert.deepEqual(labels(staffNavItems({ showEvents: true, showPayouts: true, showSettings: true })), [
    "Dashboard",
    "Events",
    "POS",
    "Payouts",
    "Settings",
  ]);
});

test("a member who is not an Org Admin sees neither Payouts nor Settings", () => {
  assert.deepEqual(labels(staffNavItems({ showEvents: true })), ["Dashboard", "Events", "POS"]);
});

// --- where the entries point ------------------------------------------------

test("the Payouts entry points at the payouts page", () => {
  const payouts = staffNavItems({ showPayouts: true }).find((item) => item.label === "Payouts");
  assert.equal(payouts?.href, "/payouts");
});
