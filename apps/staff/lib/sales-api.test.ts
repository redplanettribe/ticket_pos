import assert from "node:assert/strict";
import test from "node:test";

import {
  channelSourceLabel,
  paymentMethodLabel,
  reversalLabel,
  reversedCountLabel,
  salesExportErrorMessage,
  taxIdLabel,
} from "./sales-api.ts";

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

// --- Sale Reversal provenance ---------------------------------------------

test("reversalLabel names when the sale went and which side asked", () => {
  assert.equal(
    reversalLabel("2026-07-07T12:00:00Z", "customer", "UTC"),
    "Jul 7, 2026, 12:00 PM by Customer",
  );
  assert.equal(
    reversalLabel("2026-07-07T12:00:00Z", "staff", "UTC"),
    "Jul 7, 2026, 12:00 PM by Staff",
  );
  assert.equal(
    reversalLabel("2026-07-07T12:00:00Z", "operator", "UTC"),
    "Jul 7, 2026, 12:00 PM by The platform",
  );
});

test("reversalLabel says so plainly when nothing was recorded", () => {
  assert.equal(reversalLabel(null, null, "UTC"), "Not recorded");
});

test("reversalLabel falls back to the raw actor for one it does not know", () => {
  assert.equal(
    reversalLabel("2026-07-07T12:00:00Z", "auditor", "UTC"),
    "Jul 7, 2026, 12:00 PM by auditor",
  );
});

// --- reversed count -------------------------------------------------------

test("reversedCountLabel counts the Event's reversed sales, singular and plural", () => {
  assert.equal(reversedCountLabel(1), "1 reversed sale");
  assert.equal(reversedCountLabel(6), "6 reversed sales");
});

test("reversedCountLabel says nothing was reversed rather than showing a bare zero", () => {
  assert.equal(reversedCountLabel(0), "No reversed sales");
});

// --- the Sales Export refusal ---------------------------------------------

test("salesExportErrorMessage shows the row cap's own sentence, not the generic one", () => {
  // What the API sends when the filters match more sales than one file carries.
  // The useful sentence is the FIELD error; the envelope's own message is
  // boilerplate, and showing it would hide the only thing that helps.
  assert.equal(
    salesExportErrorMessage({
      error: {
        code: "VALIDATION_FAILED",
        message: "Request validation failed",
        details: {
          fields: [
            {
              field: "filters",
              message:
                "This Event has 24,318 matching sales; up to 10,000 can be downloaded at once. Narrow the date range and try again.",
            },
          ],
        },
      },
    }),
    "This Event has 24,318 matching sales; up to 10,000 can be downloaded at once. Narrow the date range and try again.",
  );
});

test("salesExportErrorMessage falls back to the envelope's message without field errors", () => {
  assert.equal(
    salesExportErrorMessage({ error: { code: "EVENT_NOT_FOUND", message: "Event not found." } }),
    "Event not found.",
  );
});

test("salesExportErrorMessage says something rather than nothing on an unreadable failure", () => {
  assert.equal(salesExportErrorMessage(null), "Failed to export sales");
  assert.equal(salesExportErrorMessage({ error: { details: { fields: [] } } }), "Failed to export sales");
});
