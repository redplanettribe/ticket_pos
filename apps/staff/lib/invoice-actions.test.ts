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

const NONE = { check: false, resend: false, annul: false, reissue: false };

test("a pending or parked document offers check, resend and mark annulled", () => {
  assert.deepEqual(invoiceLevers("pending", true), { ...NONE, check: true, resend: true, annul: true });
  assert.deepEqual(invoiceLevers("needs_attention", true), { ...NONE, check: true, resend: true, annul: true });
});

// A manual Tax Invoice the SRI refused keeps the two remedies it always had
// and is not offered Mark annulled: nothing about it was ever valid at the SRI.
test("a refused manual document keeps check and resend without mark annulled", () => {
  assert.deepEqual(invoiceLevers("not_authorized", true), { ...NONE, check: true, resend: true });
  assert.deepEqual(invoiceLevers("rejected", true), { ...NONE, check: true, resend: true });
});

test("a finished document offers nothing", () => {
  for (const status of ["authorized", "annulled", "withdrawn"] as const) {
    const levers = invoiceLevers(status, true);
    assert.deepEqual(levers, NONE, status);
    assert.equal(hasInvoiceLevers(levers), false, status);
  }
});

// The Sale Invoice Reissue (#483, ADR 0061): the one lever an authorized
// document has, and only a Sale Invoice that is the Sale's current one.
const CURRENT_SALE_INVOICE = { kind: "sale", superseded_by_invoice_id: null, credited_by_invoice_id: null } as const;

test("a current authorized Sale Invoice offers reissue and nothing else", () => {
  const levers = invoiceLevers("authorized", true, CURRENT_SALE_INVOICE);
  assert.deepEqual(levers, { ...NONE, reissue: true });
  // Reissue is its own card, not part of the SRI-remedies card.
  assert.equal(hasInvoiceLevers(levers), false);
});

test("reissue is offered only on a Sale Invoice", () => {
  assert.equal(invoiceLevers("authorized", true, { ...CURRENT_SALE_INVOICE, kind: "manual" }).reissue, false);
  assert.equal(invoiceLevers("authorized", true, { ...CURRENT_SALE_INVOICE, kind: "credit_note" }).reissue, false);
  assert.equal(invoiceLevers("authorized", true).reissue, false);
});

test("reissue is offered only where the Sale Invoice is authorized", () => {
  for (const status of ["owed", "pending", "needs_attention", "withdrawn", "annulled", "not_authorized", "rejected"] as const) {
    assert.equal(invoiceLevers(status, status !== "owed", CURRENT_SALE_INVOICE).reissue, false, status);
  }
});

// A superseded factura points at the current one; a credited factura is a
// reversed Sale's, or one with a reissue in flight or settled — the API
// refuses each by code, so none is offered.
test("reissue is not offered on a superseded or credited Sale Invoice", () => {
  assert.equal(
    invoiceLevers("authorized", true, { ...CURRENT_SALE_INVOICE, superseded_by_invoice_id: "corrected" }).reissue,
    false,
  );
  assert.equal(
    invoiceLevers("authorized", true, { ...CURRENT_SALE_INVOICE, credited_by_invoice_id: "note" }).reissue,
    false,
  );
});

// A reissue that died (#484): the API reports the factura credited by no
// live Credit Note and superseded by nothing — the annulled Credit Note and
// the withdrawn corrected factura stay on file without a link — and the
// lever is offered again exactly as on a factura never reissued.
test("reissue is offered again once the reissue's Credit Note died", () => {
  const afterADeadReissue = { kind: "sale", superseded_by_invoice_id: null, credited_by_invoice_id: null } as const;
  assert.deepEqual(invoiceLevers("authorized", true, afterADeadReissue), { ...NONE, reissue: true });
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
    // Who holds the document, said in both languages (#514, #516): the
    // Attempts row for an answer that found no record of the clave, and the
    // two banners that must never both be shown.
    "invoicingAttemptOutcomeUnknown",
    "invoicingCheckStatusHintTitle",
    "invoicingCheckStatusHint",
    "invoicingResendHintTitle",
    "invoicingResendHint",
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
    "invoicingReissue",
    "invoicingReissuing",
    "invoicingReissueCardTitle",
    "invoicingReissueCardDescription",
    "invoicingReissueDialogTitle",
    "invoicingReissueDialogDescription",
    "invoicingReissueEmailHint",
    "invoicingReissueNote",
    "invoicingReissueNoteHint",
    "invoicingReissueCancel",
    "invoicingReissueDone",
    "invoicingReissueFailed",
    "invoicingSupersededBadge",
    "invoicingDetailChainTitle",
    "invoicingDetailSupersedes",
    "invoicingDetailSupersededBy",
    "invoicingDetailCreditsLink",
    "invoicingDetailCreditedByLink",
    "invoicingReissueTrailTitle",
    "invoicingReissueTrail",
    "invoicingReissueTrailNote",
  ];
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: Record<string, string>; errors: { envelope: Record<string, string> } };
    for (const key of keys) {
      assert.ok(catalog.operator[key]?.trim(), `${locale}: operator.${key} is missing or empty`);
    }
    for (const code of [
      "INVOICE_NOT_ANNULLABLE",
      "INVOICE_ANNULLED",
      "INVOICE_WITHDRAWN",
      "INVOICE_MANUAL_NOT_REISSUABLE",
      "CREDIT_NOTE_NOT_REISSUABLE",
      "INVOICE_NOT_AUTHORIZED",
      "INVOICE_SALE_REVERSED",
      "REISSUE_IN_FLIGHT",
      "INVOICE_SUPERSEDED",
      "INVOICE_ALREADY_CREDITED",
    ]) {
      assert.ok(catalog.errors.envelope[code]?.trim(), `${locale}: errors.envelope.${code} is missing`);
    }
  }
});
