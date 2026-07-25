import assert from "node:assert/strict";
import test from "node:test";

import { channelSourceLabel, paymentMethodLabel } from "./sales-api.ts";

// --- payment method labels ------------------------------------------------

test("paymentMethodLabel names every recorded Payment Method", () => {
  assert.equal(paymentMethodLabel("cash"), "Cash");
  assert.equal(paymentMethodLabel("transfer"), "Transfer");
  assert.equal(paymentMethodLabel("payphone"), "PayPhone");
});

test("paymentMethodLabel renders a dash for a sale without one", () => {
  assert.equal(paymentMethodLabel(null), "—");
});

test("paymentMethodLabel falls back to the raw value for an unknown method", () => {
  assert.equal(paymentMethodLabel("barter"), "barter");
});

// --- channel/source labels ------------------------------------------------

test("channelSourceLabel renders an Online Sale without a source", () => {
  assert.equal(channelSourceLabel("online", null), "Online");
});

test("channelSourceLabel joins channel and source for imported sales", () => {
  assert.equal(channelSourceLabel("import", "direct"), "Import · Direct");
});
