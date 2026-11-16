import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  ANNULMENT_CHECK_BADGE,
  ANNULMENT_PANEL_REFUSALS,
  ANNULMENT_PORTAL_STEPS,
  SRI_EN_LINEA_URL,
  annulmentCheckLine,
  annulmentCheckOffersCheckStatus,
  annulmentPanelMode,
  annulmentPortalFields,
  annulmentPortalPathKey,
  annulmentRecordReady,
  annulmentSendsNotice,
  annulmentWindowProgress,
  annulmentWindowTone,
  visibleWhitespace,
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

// The Annulment Check on the annulled face (#746): one state line per verdict,
// the badge, and the Check status row whenever the check is not confirmed.

const CHECK = {
  verdict: "checking" as const,
  scheduled: true,
  since: null,
  next_ask_at: "2026-07-07T19:17:00Z",
  asks_until: "2026-08-08T05:00:00Z",
  asks: [],
  answers: [],
};
const ANNULLED_AT = "2026-07-07T19:02:00Z";

test("checking reads since the recording, with the next ask", () => {
  assert.deepEqual(annulmentCheckLine(CHECK, ANNULLED_AT), {
    kind: "checking",
    since: ANNULLED_AT,
    next: "2026-07-07T19:17:00Z",
  });
});

test("confirmed reads ANULADO at the ask that read it", () => {
  assert.deepEqual(
    annulmentCheckLine({ ...CHECK, verdict: "confirmed", since: "2026-07-07T19:17:00Z", next_ask_at: null, asks_until: null }, ANNULLED_AT),
    { kind: "confirmed", at: "2026-07-07T19:17:00Z" },
  );
});

test("mismatch reads still authorized since the ask that settled it", () => {
  assert.deepEqual(
    annulmentCheckLine({ ...CHECK, verdict: "mismatch", since: "2026-07-07T19:32:00Z" }, ANNULLED_AT),
    { kind: "mismatch", since: "2026-07-07T19:32:00Z" },
  );
});

test("unconfirmed reads asking every 6 h until the schedule's end, or that the asks have ended", () => {
  const unconfirmed = { ...CHECK, verdict: "unconfirmed" as const, since: "2026-07-07T20:02:00Z" };
  assert.deepEqual(annulmentCheckLine(unconfirmed, ANNULLED_AT), {
    kind: "unconfirmed",
    at: "2026-07-07T20:02:00Z",
    until: "2026-08-08T05:00:00Z",
  });
  assert.deepEqual(annulmentCheckLine({ ...unconfirmed, next_ask_at: null }, ANNULLED_AT), {
    kind: "unconfirmed",
    at: "2026-07-07T20:02:00Z",
    until: null,
  });
});

test("an annulment recorded before the check existed reads unchecked", () => {
  assert.deepEqual(
    annulmentCheckLine({ ...CHECK, verdict: "unconfirmed", scheduled: false, next_ask_at: null, asks_until: null }, ANNULLED_AT),
    { kind: "unchecked" },
  );
});

test("Check status is offered whenever the check is not confirmed", () => {
  for (const verdict of ["checking", "mismatch", "unconfirmed"] as const) {
    assert.equal(annulmentCheckOffersCheckStatus({ ...CHECK, verdict }), true, verdict);
  }
  assert.equal(annulmentCheckOffersCheckStatus({ ...CHECK, verdict: "confirmed" }), false);
});

test("the badge's tone follows the verdict", () => {
  assert.equal(ANNULMENT_CHECK_BADGE.confirmed, "default");
  assert.equal(ANNULMENT_CHECK_BADGE.mismatch, "destructive");
  assert.equal(ANNULMENT_CHECK_BADGE.checking, "secondary");
  assert.equal(ANNULMENT_CHECK_BADGE.unconfirmed, "outline");
});

// The portal runbook on the record face (#752): the numbered steps, the menu
// path by the factura's environment, the fields to copy exactly, the receptor
// ID with its spaces drawn, and the two lines about what recording causes.

const PORTAL = {
  access_key: "0707202601179001234500110010010000000421234567811",
  authorization_date: "2026-07-07T19:02:11Z",
  authorization_day: "07/07/2026",
  receptor_id: "1712345678 001",
  receptor_email: "buyer@example.com",
  environment: "production" as const,
};

test("the checklist walks sign in, the form, the fields and Aceptar, starting at SRI en linea", () => {
  assert.deepEqual(ANNULMENT_PORTAL_STEPS, ["sign_in", "open_form", "fill", "accept"]);
  assert.match(SRI_EN_LINEA_URL, /^https:\/\/srienlinea\.sri\.gob\.ec\//);
});

test("the menu path follows the factura's environment", () => {
  assert.equal(annulmentPortalPathKey("production"), "invoicingAnnulPortalPathProduction");
  assert.equal(annulmentPortalPathKey("test"), "invoicingAnnulPortalPathTest");
  for (const locale of ["en", "es"]) {
    const catalog = loadCatalog(locale);
    assert.match(catalog.operator.invoicingAnnulPortalPathProduction, /Producci[oó]n/, locale);
    assert.match(catalog.operator.invoicingAnnulPortalPathTest, /Pruebas/, locale);
  }
});

test("the fields copy the clave, the authorization day, the receptor ID and the email exactly", () => {
  assert.deepEqual(annulmentPortalFields(PORTAL), [
    { field: "access_key", value: PORTAL.access_key, spaces: false },
    { field: "authorization_day", value: "07/07/2026", spaces: false },
    { field: "receptor_id", value: "1712345678 001", spaces: true },
    { field: "receptor_email", value: "buyer@example.com", spaces: false },
  ]);
});

test("a document not yet authorized has no authorization date, and a Recipient with no email no email row", () => {
  const fields = annulmentPortalFields({ ...PORTAL, authorization_date: null, authorization_day: null, receptor_email: "" });
  assert.deepEqual(
    fields.map((f) => f.field),
    ["access_key", "receptor_id"],
  );
});

test("the receptor ID keeps every space, each drawn on its own", () => {
  const segments = visibleWhitespace(" 1712 345678  001 ");
  assert.equal(segments.map((s) => s.text).join(""), " 1712 345678  001 ");
  assert.deepEqual(
    segments.map((s) => (s.space ? "_" : s.text)),
    ["_", "1712", "_", "345678", "_", "_", "001", "_"],
  );
  assert.deepEqual(visibleWhitespace("1712345678001"), [{ text: "1712345678001", space: false }]);
  assert.deepEqual(visibleWhitespace(""), []);
  assert.deepEqual(
    visibleWhitespace("a\tb").map((s) => s.space),
    [false, true, false],
  );
});

test("a Sale Invoice's panel says the buyer is sent an Annulment Notice, a manual Tax Invoice's does not", () => {
  assert.equal(annulmentSendsNotice("sale"), true);
  assert.equal(annulmentSendsNotice("manual"), false);
  assert.equal(annulmentSendsNotice("credit_note"), false);
});

test("the runbook has its words in both languages", () => {
  const keys = [
    "invoicingAnnulPortalTitle",
    "invoicingAnnulPortalHint",
    "invoicingAnnulPortalStepSignIn",
    "invoicingAnnulPortalOpen",
    "invoicingAnnulPortalStepOpenForm",
    "invoicingAnnulPortalStepFill",
    "invoicingAnnulPortalStepAccept",
    "invoicingAnnulPortalStepNumber",
    "invoicingAnnulPortalFieldAccessKey",
    "invoicingAnnulPortalFieldAuthorizationDay",
    "invoicingAnnulPortalFieldReceptorId",
    "invoicingAnnulPortalFieldReceptorEmail",
    "invoicingAnnulPortalSpace",
    "invoicingAnnulPortalCopy",
    "invoicingAnnulPortalCopied",
    "invoicingAnnulPortalCopyFailed",
    "invoicingAnnulPanelNotice",
    "invoicingAnnulPanelArchiveAgain",
  ];
  for (const locale of ["en", "es"]) {
    const catalog = loadCatalog(locale);
    for (const key of keys) {
      assert.ok(catalog.operator[key], `${locale}: operator.${key}`);
    }
  }
});
