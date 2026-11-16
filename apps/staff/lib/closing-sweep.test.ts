import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  UNRECORDED_ANNULMENT_KEYS,
  closingSweepLine,
  closingSweepMonthLabel,
  unrecordedAnnulmentOffer,
} from "./closing-sweep.ts";

// The Closing Sweep and the Unrecorded Annulment on the staff app (#750, spec
// #742, ADR 0078): the dashboard's line and what a flagged factura's panel
// offers. The API decides every fact; these decide only what to draw.

const FOUND = "2026-08-08T05:00:00Z";
const flag = { found_at: FOUND, attempt_id: 7, acknowledged_by: null, acknowledged_at: null };

test("no warning on a factura the sweep has not flagged", () => {
  assert.equal(unrecordedAnnulmentOffer({ unrecorded_annulment: null, annulment: null }), null);
});

test("a flagged factura Mark annulled can record is offered the record", () => {
  const offer = unrecordedAnnulmentOffer({
    unrecorded_annulment: flag,
    annulment: { acknowledge: { admitted: false, refusal: { code: "INVOICE_ANNUL_INSTEAD" } } },
  });
  assert.deepEqual(offer, { kind: "record", foundAt: FOUND });
});

test("a flagged factura a live Credit Note keeps from Mark annulled is offered the acknowledgement", () => {
  const offer = unrecordedAnnulmentOffer({
    unrecorded_annulment: flag,
    annulment: { acknowledge: { admitted: true, refusal: null } },
  });
  assert.deepEqual(offer, { kind: "acknowledge", foundAt: FOUND });
});

test("an acknowledged flag says who and when, and offers nothing", () => {
  const offer = unrecordedAnnulmentOffer({
    unrecorded_annulment: { ...flag, acknowledged_by: "ana@example.com", acknowledged_at: "2026-08-10T14:30:00Z" },
    annulment: { acknowledge: { admitted: false, refusal: { code: "INVOICE_UNRECORDED_ANNULMENT_ACKNOWLEDGED" } } },
  });
  assert.deepEqual(offer, {
    kind: "acknowledged",
    foundAt: FOUND,
    by: "ana@example.com",
    at: "2026-08-10T14:30:00Z",
  });
});

test("the dashboard says nothing while every factura was swept", () => {
  assert.equal(closingSweepLine({ month: "2026-07", unswept_count: 0, due_again_count: 0 }), null);
});

test("the dashboard counts the unswept facturas, and offers Sweep again until all are due", () => {
  assert.deepEqual(closingSweepLine({ month: "2026-07", unswept_count: 3, due_again_count: 0 }), {
    month: "2026-07",
    count: 3,
    sweepAgain: true,
  });
  assert.deepEqual(closingSweepLine({ month: "2026-07", unswept_count: 3, due_again_count: 3 }), {
    month: "2026-07",
    count: 3,
    sweepAgain: false,
  });
});

test("the month reads by name in the reader's language, never shifted by a zone", () => {
  assert.equal(closingSweepMonthLabel("2026-07", "en"), "July 2026");
  assert.equal(closingSweepMonthLabel("2026-01", "es"), "enero de 2026");
  assert.equal(closingSweepMonthLabel("nonsense", "en"), "nonsense");
});

type Catalog = { operator: Record<string, string>; errors: { envelope: Record<string, string> } };

function loadCatalog(locale: string): Catalog {
  return JSON.parse(readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8")) as Catalog;
}

test("the sweep and the warning have their words in both languages", () => {
  const keys = [
    ...Object.values(UNRECORDED_ANNULMENT_KEYS),
    "invoicingUnrecordedBadge",
    "invoicingUnrecordedTitle",
    "invoicingUnrecordedFound",
    "invoicingUnrecordedAcknowledgeSubmit",
    "invoicingUnrecordedAcknowledgeSubmitting",
    "invoicingUnrecordedAcknowledgeDone",
    "invoicingUnrecordedAcknowledgeFailed",
    "invoicingUnrecordedAcknowledgeOpen",
    "invoicingUnrecordedAcknowledgeCancel",
    "invoicingAttentionUnrecordedAnnulment",
    "invoicingAnnulPanelFromUnrecorded",
    "closingSweepCardTitle",
    "closingSweepUnswept",
    "closingSweepDescription",
    "closingSweepAgain",
    "closingSweepAgainBusy",
    "closingSweepAgainDone",
    "closingSweepAgainFailed",
    "closingSweepDueAgain",
  ];
  for (const locale of ["en", "es"]) {
    const catalog = loadCatalog(locale);
    for (const key of keys) {
      assert.ok(catalog.operator[key], `${locale}: operator.${key}`);
    }
    for (const code of [
      "INVOICE_NO_UNRECORDED_ANNULMENT",
      "INVOICE_UNRECORDED_ANNULMENT_ACKNOWLEDGED",
      "INVOICE_ANNUL_INSTEAD",
    ]) {
      assert.ok(catalog.errors.envelope[code], `${locale}: errors.envelope.${code}`);
    }
  }
});
