import assert from "node:assert/strict";
import test from "node:test";

import { channelSourceLabel, paymentMethodLabel, taxIdLabel } from "./sales-api.ts";

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

// --- Tax ID snapshot ------------------------------------------------------

test("taxIdLabel names every Tax ID Type beside its number, unmasked", () => {
  assert.equal(taxIdLabel("cedula", "1712345675"), "Cédula: 1712345675");
  assert.equal(taxIdLabel("ruc", "1790012346001"), "RUC: 1790012346001");
  assert.equal(taxIdLabel("passport", "XZ998877"), "Pasaporte: XZ998877");
});

test("taxIdLabel renders a dash for a sale recorded without a Tax ID", () => {
  assert.equal(taxIdLabel(null, null), "—");
});

test("taxIdLabel renders a dash rather than half a Tax ID", () => {
  assert.equal(taxIdLabel("cedula", null), "—");
  assert.equal(taxIdLabel(null, "1712345675"), "—");
});

test("taxIdLabel falls back to the raw type for one it does not know", () => {
  assert.equal(taxIdLabel("dni", "1712345675"), "dni: 1712345675");
});
