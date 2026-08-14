import assert from "node:assert/strict";
import test from "node:test";

import { staffNavItems } from "../src/lib/staff-nav.ts";

/**
 * The panel's entries are asserted by KEY, not by the word each one is drawn
 * with. What this module decides is which entries exist, for whom, and in what
 * order; the words are the staff catalog's, in whichever of two languages the
 * reader is owed (ADR 0041). Asserting "Payouts" here would have made a copy
 * edit — or the Spanish translation — break a test about permissions.
 */
const keys = (items: Array<{ key: string }>) => items.map((item) => item.key);

// --- what everybody sees ---------------------------------------------------

test("a session with no organization sees only the unconditional entries", () => {
  assert.deepEqual(keys(staffNavItems({})), ["dashboard"]);
});

test("POS is still a placeholder, so the panel does not advertise it", () => {
  const everything = staffNavItems({ showEvents: true, showPayouts: true, showSettings: true });
  assert.ok(!keys(everything).includes("pos"));
});

// --- the gated entries -----------------------------------------------------

test("Events appears for a member of an organization", () => {
  assert.deepEqual(keys(staffNavItems({ showEvents: true })), ["dashboard", "events"]);
});

test("an Org Admin sees Payouts between Events and Settings", () => {
  assert.deepEqual(keys(staffNavItems({ showEvents: true, showPayouts: true, showSettings: true })), [
    "dashboard",
    "events",
    "payouts",
    "settings",
  ]);
});

test("a member who is not an Org Admin sees neither Payouts nor Settings", () => {
  assert.deepEqual(keys(staffNavItems({ showEvents: true })), ["dashboard", "events"]);
});

// --- where the entries point ------------------------------------------------

test("the Payouts entry points at the payouts page", () => {
  const payouts = staffNavItems({ showPayouts: true }).find((item) => item.key === "payouts");
  assert.equal(payouts?.href, "/payouts");
});
