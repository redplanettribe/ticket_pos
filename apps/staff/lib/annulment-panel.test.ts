import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  ANNULMENT_PANEL_REFUSALS,
  annulmentPanelMode,
  annulmentRecordReady,
  annulmentWindowProgress,
  annulmentWindowTone,
} from "./annulment-panel.ts";

// The annulment panel on a factura's detail page (#745, spec #742, ADR 0078):
// which face it shows, how far along its window the timeline marks today,
// which tone the "Annullable until" line takes, and when the record form may
// be pressed. Every date and every rule comes from the API's `annulability`;
// these decide only what to draw from it.

const OPEN = {
  issued_on: "2026-07-07",
  deadline: "2026-08-07",
  warning_from: "2026-08-01",
  today: "2026-07-17",
  closes_at: "2026-08-08T05:00:00Z",
  open: true,
  warning: false,
  days_left: 21,
  days_total: 31,
};

const admitted = { admitted: true, refusal: null, window: OPEN, portal: null };
const refused = (code: string) => ({ admitted: false, refusal: { code, message: "no" }, window: OPEN, portal: null });

test("an admitted factura shows the record form", () => {
  assert.equal(annulmentPanelMode({ status: "authorized", annulability: admitted }), "record");
  assert.equal(annulmentPanelMode({ status: "pending", annulability: admitted }), "record");
});

test("an annulled factura shows the annulment, whatever the API says of a second press", () => {
  assert.equal(annulmentPanelMode({ status: "annulled", annulability: refused("INVOICE_NOT_ANNULLABLE") }), "annulled");
});

test("a factura refused for a fact about it shrinks to one sentence", () => {
  for (const code of ANNULMENT_PANEL_REFUSALS) {
    assert.equal(annulmentPanelMode({ status: "authorized", annulability: refused(code) }), "refused", code);
  }
});

// A document in a state Mark annulled never reaches - owed, withdrawn,
// abandoned, refused manual - is not offered a panel at all, and a Credit
// Note has no annulability: it keeps the SRI-remedies card's button.
test("no panel where the act is out of reach, nor on a Credit Note", () => {
  assert.equal(annulmentPanelMode({ status: "owed", annulability: refused("INVOICE_NOT_ISSUED") }), null);
  assert.equal(annulmentPanelMode({ status: "withdrawn", annulability: refused("INVOICE_NOT_ANNULLABLE") }), null);
  assert.equal(annulmentPanelMode({ status: "pending", annulability: null }), null);
});

test("the timeline marks today along the window, and its end once closed", () => {
  assert.equal(annulmentWindowProgress(OPEN), 10 / 31);
  assert.equal(annulmentWindowProgress({ ...OPEN, days_left: 31 }), 0);
  assert.equal(annulmentWindowProgress({ ...OPEN, days_left: 0, warning: true }), 1);
  assert.equal(annulmentWindowProgress({ ...OPEN, open: false, days_left: 0 }), 1);
  assert.equal(annulmentWindowProgress({ ...OPEN, days_total: 0, days_left: 0 }), 1);
});

test("the line is quiet, then a warning from day 1, then closed", () => {
  assert.equal(annulmentWindowTone(OPEN), "open");
  assert.equal(annulmentWindowTone({ ...OPEN, warning: true }), "warning");
  assert.equal(annulmentWindowTone({ ...OPEN, open: false, warning: false }), "closed");
});

test("the record form needs the number in full, and past the window the acknowledgement", () => {
  const base = { number: "001-001-000000012", typedNumber: "001-001-000000012", late: false, typedAcknowledgement: "", phrase: "annulled at the portal" };
  assert.equal(annulmentRecordReady(base), true);
  assert.equal(annulmentRecordReady({ ...base, typedNumber: "  001-001-000000012 " }), true);
  assert.equal(annulmentRecordReady({ ...base, typedNumber: "000000012" }), false);
  assert.equal(annulmentRecordReady({ ...base, typedNumber: "" }), false);
  assert.equal(annulmentRecordReady({ ...base, late: true }), false);
  assert.equal(annulmentRecordReady({ ...base, late: true, typedAcknowledgement: " Annulled at the Portal " }), true);
  assert.equal(annulmentRecordReady({ ...base, late: true, typedAcknowledgement: "yes" }), false);
});

type Catalog = { operator: Record<string, string>; errors: { envelope: Record<string, string> } };

function loadCatalog(locale: string): Catalog {
  return JSON.parse(readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8")) as Catalog;
}

test("the panel has its words in both languages", () => {
  const keys = [
    "invoicingAnnulPanelTitle",
    "invoicingAnnulPanelUntil",
    "invoicingAnnulPanelUntilToday",
    "invoicingAnnulPanelClosed",
    "invoicingAnnulPanelIssued",
    "invoicingAnnulPanelDeadline",
    "invoicingAnnulPanelDaysLeft",
    "invoicingAnnulPanelLastDay",
    "invoicingAnnulPanelWindowClosed",
    "invoicingAnnulPanelRecordTitle",
    "invoicingAnnulPanelRecordHint",
    "invoicingAnnulPanelNumberLabel",
    "invoicingAnnulPanelLateLabel",
    "invoicingAnnulPanelLatePhrase",
    "invoicingAnnulPanelNoteLabel",
    "invoicingAnnulPanelNotePlaceholder",
    "invoicingAnnulPanelReminder",
    "invoicingAnnulPanelSubmit",
    "invoicingAnnulPanelSubmitting",
    "invoicingAnnulPanelDone",
    "invoicingAnnulPanelFailed",
    "invoicingAnnulPanelRefusedTitle",
    "invoicingAnnulPanelAnnulledTitle",
    "invoicingAnnulPanelTrail",
    "invoicingAnnulPanelTrailNote",
    "invoicingAnnulPanelTrailLate",
    "invoicingAnnulPanelTrailFrom",
  ];
  for (const locale of ["en", "es"]) {
    const catalog = loadCatalog(locale);
    for (const key of keys) {
      assert.ok(catalog.operator[key], `${locale}: operator.${key}`);
    }
    for (const code of [...ANNULMENT_PANEL_REFUSALS, "INVOICE_NUMBER_MISMATCH", "INVOICE_ANNULMENT_LATE", "INVOICE_ANNULMENT_NOT_LATE"]) {
      assert.ok(catalog.errors.envelope[code], `${locale}: errors.envelope.${code}`);
    }
  }
});
