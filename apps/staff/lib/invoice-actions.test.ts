import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { hasInvoiceLevers, invoiceLevers } from "./invoice-actions.ts";

// WHAT THESE ASSERT. The card's sentences live in the catalogs (ADR 0041);
// what is left is the decision they hang on: which levers a document in
// each state is offered. The API is the gate — every refusal is
// integration-tested against it — and the last test is what makes sure the
// queue, the annulment and the trail cannot reach an operator with nothing
// to say in either language.

test("a pending or parked document offers check, resend and mark annulled", () => {
  assert.deepEqual(invoiceLevers("pending", true), { check: true, resend: true, annul: true });
  assert.deepEqual(invoiceLevers("needs_attention", true), { check: true, resend: true, annul: true });
});

// A manual Tax Invoice the SRI refused keeps the two remedies it always had
// and is not offered Mark annulled: nothing about it was ever valid at the SRI.
test("a refused manual document keeps check and resend without mark annulled", () => {
  assert.deepEqual(invoiceLevers("not_authorized", true), { check: true, resend: true, annul: false });
  assert.deepEqual(invoiceLevers("rejected", true), { check: true, resend: true, annul: false });
});

test("a finished document offers nothing", () => {
  for (const status of ["authorized", "annulled", "withdrawn"] as const) {
    const levers = invoiceLevers(status, true);
    assert.deepEqual(levers, { check: false, resend: false, annul: false }, status);
    assert.equal(hasInvoiceLevers(levers), false, status);
  }
});

// An owed document, and one parked because it could not be signed, has no
// number at the SRI: nothing to check, resend or annul until the Drainer
// signs it.
test("an unsigned document offers nothing whatever its state", () => {
  assert.equal(hasInvoiceLevers(invoiceLevers("owed", false)), false);
  assert.equal(hasInvoiceLevers(invoiceLevers("needs_attention", false)), false);
});

test("every surface has its words in both languages", () => {
  const keys = [
    "attentionCardTitle",
    "attentionCardDescription",
    "openTheAttentionQueue",
    "invoicingAttentionTitle",
    "invoicingAttentionDescription",
    "invoicingAttentionEmpty",
    "invoicingAttentionColSince",
    "invoicingAttentionColMessages",
    "invoicingAttentionNoMessages",
    "invoicingKindFilterLabel",
    "invoicingKindFilterAll",
    "invoicingMarkAnnulled",
    "invoicingMarkingAnnulled",
    "invoicingMarkAnnulledConfirmTitle",
    "invoicingMarkAnnulledConfirm",
    "invoicingMarkAnnulledDone",
    "invoicingMarkAnnulledFailed",
    "invoicingAnnulmentTitle",
    "invoicingAnnulmentTrail",
    "saleDocumentsTitle",
    "saleDocumentsDescription",
    "saleDocumentsNone",
  ];
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: Record<string, string>; errors: { envelope: Record<string, string> } };
    for (const key of keys) {
      assert.ok(catalog.operator[key]?.trim(), `${locale}: operator.${key} is missing or empty`);
    }
    for (const code of ["INVOICE_NOT_ANNULLABLE", "INVOICE_ANNULLED", "INVOICE_WITHDRAWN"]) {
      assert.ok(catalog.errors.envelope[code]?.trim(), `${locale}: errors.envelope.${code} is missing`);
    }
  }
});
