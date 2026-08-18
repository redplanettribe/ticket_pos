import assert from "node:assert/strict";
import test from "node:test";

import {
  ECUADOR_TIME_ZONE,
  formatEventDateShort,
  formatEventDateTime,
  formatEventTime,
  formatPrice,
  formatReversalDeadline,
  priceFrom,
} from "./format.ts";

// An Event at 18:00 on a Sunday in Guayaquil, stated as the instant the API sends.
const SUNDAY_EVENING = "2026-07-12T23:00:00Z";
// Inside the Reversal Window's 20:00 Ecuador cutoff on Tuesday the 7th.
const REVERSAL_CUTOFF = "2026-07-08T01:00:00Z";

// The U+00A0 inside Spanish's "p. m." below is asserted exactly, and is the
// Storefront's own doing rather than ICU's. ICU disagrees with itself about that
// space across versions — U+00A0 under the ICU 76 in Node 22, a plain space
// under the ICU 78 in the Node 26 the containers run — so format.ts states it
// instead of inheriting it. That is what makes an exact assertion safe here: it
// pins a contract this repo keeps, not a codepoint the runtime happens to pick,
// and it is the assertion that would have caught the no-break space going
// missing at the Node 26 upgrade.

test("en-US prices are unchanged: whole amounts drop the cents, the rest keep them", () => {
  assert.equal(formatPrice(2500, "USD"), "$25");
  assert.equal(formatPrice(123450, "USD"), "$1,234.50");
  assert.equal(formatPrice(0, "USD"), "$0");
});

test("es-EC prices use Ecuadorian number conventions", () => {
  // Same currency, same dollar sign — only the grouping and decimal marks move.
  assert.equal(formatPrice(2500, "USD", "es-EC"), "$25");
  assert.equal(formatPrice(123450, "USD", "es-EC"), "$1.234,50");
});

test("the 'from' price is offered as data a message template can render", () => {
  // The copy is the caller's problem; this side only decides which sentence is
  // owed and formats the amount that goes in it.
  assert.deepEqual(priceFrom(2500, "USD"), { kind: "from", price: "$25" });
  assert.deepEqual(priceFrom(0, "USD"), { kind: "free" });
  assert.equal(priceFrom(null, "USD"), null);
  assert.deepEqual(priceFrom(123450, "USD", "es-EC"), { kind: "from", price: "$1.234,50" });
});

test("en-US long Event dates are unchanged", () => {
  assert.equal(
    formatEventDateTime(SUNDAY_EVENING, "America/Guayaquil"),
    "Sunday, July 12, 2026 · 6:00 PM",
  );
  assert.equal(formatEventDateTime(null, "America/Guayaquil"), null);
  assert.equal(formatEventDateTime("not-a-date", "America/Guayaquil"), null);
});

test("es-EC long Event dates are Spanish", () => {
  assert.equal(
    formatEventDateTime(SUNDAY_EVENING, "America/Guayaquil", "es-EC"),
    "domingo, 12 de julio de 2026 · 6:00 p.\u00a0m.",
  );
});

test("en-US short Event dates are unchanged", () => {
  assert.equal(formatEventDateShort(SUNDAY_EVENING, "America/Guayaquil"), "Sun Jul 12, 6:00 PM");
  assert.equal(formatEventDateShort(null, "America/Guayaquil"), null);
  assert.equal(formatEventDateShort("not-a-date", "America/Guayaquil"), null);
});

test("the time-only form is the short form's time slot and nothing else", () => {
  // What a Timeline card says under a Day Bucket header: the date is the
  // header's to say, so the card keeps only the hour — in the Event's own
  // timezone, in the page's language, with the same "PM" spacing format()
  // has always produced.
  assert.equal(formatEventTime(SUNDAY_EVENING, "America/Guayaquil"), "6:00 PM");
  assert.equal(formatEventTime(SUNDAY_EVENING, "Europe/Madrid"), "1:00 AM");
  assert.equal(formatEventTime(SUNDAY_EVENING, "America/Guayaquil", "es-EC"), "6:00 p.\u00a0m.");
  assert.equal(formatEventTime(null, "America/Guayaquil"), null);
  assert.equal(formatEventTime("not-a-date", "America/Guayaquil"), null);
});

test("es-EC short Event dates are Spanish, in Spanish part order", () => {
  // Day before month, lowercase abbreviations, "p. m." for the afternoon.
  assert.equal(
    formatEventDateShort(SUNDAY_EVENING, "America/Guayaquil", "es-EC"),
    "dom 12 jul, 6:00 p.\u00a0m.",
  );
});

test("the short form carries no en-US artefacts into es-EC", () => {
  // The old implementation stripped a comma and swapped an English " at " out of
  // the formatted string, which is en-US shaped surgery: under es-EC it removed
  // the separator Spanish puts between the day and the month.
  const label = formatEventDateShort(SUNDAY_EVENING, "America/Guayaquil", "es-EC") ?? "";
  assert.ok(!label.includes(" at "), `English " at " leaked into ${label}`);
  assert.ok(!label.includes(",,"), `stray comma artefact in ${label}`);
  assert.ok(!/\s,/.test(label), `orphaned comma in ${label}`);
  assert.ok(!/,\s*$/.test(label), `trailing comma in ${label}`);
});

test("the Reversal Window deadline stays on Ecuador's clock in every language", () => {
  // 01:00 UTC is 20:00 the previous day in Guayaquil. The 20:00 cutoff is an
  // Ecuadorian wall-clock rule (ADR 0018), so the hour must not move with the
  // language the buyer reads it in.
  assert.equal(formatReversalDeadline(REVERSAL_CUTOFF), "Tue Jul 7, 8:00 PM");
  assert.equal(formatReversalDeadline(REVERSAL_CUTOFF, "es-EC"), "mar 7 jul, 8:00 p.\u00a0m.");
  assert.equal(ECUADOR_TIME_ZONE, "America/Guayaquil");
});

test("an Event's time stays in the Event's own timezone in every language", () => {
  // Same instant, two Events: the wall time follows the Event's timezone field
  // and never the reader's language.
  assert.equal(formatEventDateShort(SUNDAY_EVENING, "Europe/Madrid"), "Mon Jul 13, 1:00 AM");
  assert.equal(
    formatEventDateShort(SUNDAY_EVENING, "Europe/Madrid", "es-EC"),
    "lun 13 jul, 1:00 a.\u00a0m.",
  );
  assert.equal(
    formatEventDateTime(SUNDAY_EVENING, "Europe/Madrid", "es-EC"),
    "lunes, 13 de julio de 2026 · 1:00 a.\u00a0m.",
  );
});
