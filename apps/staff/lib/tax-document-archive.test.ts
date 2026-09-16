import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  isTaxDocumentArchiveRangeComplete,
  previousEcuadorCalendarMonth,
  startingTaxDocumentArchiveRange,
  taxDocumentArchiveUrl,
} from "./tax-document-archive.ts";

// WHAT THESE ASSERT (#630, spec #629). The Download XML dialog starts on the
// range the operator most likely wants — the list's own Emission Date range
// when one is set, otherwise last month in Ecuador — and turns a range into
// the download's address. The month is Ecuador's, not the browser's or UTC's:
// late on the first of a month in UTC it is still the last day of the
// previous month in Guayaquil.

const noFilter = { issuedFrom: "", issuedTo: "" };

test("with no list range the dialog starts on the previous calendar month", () => {
  const now = new Date("2026-09-16T15:00:00Z");
  assert.deepEqual(startingTaxDocumentArchiveRange(noFilter, now), { from: "2026-08-01", to: "2026-08-31" });
});

test("in January the previous month is last year's December", () => {
  const now = new Date("2027-01-10T15:00:00Z");
  assert.deepEqual(previousEcuadorCalendarMonth(now), { from: "2026-12-01", to: "2026-12-31" });
});

test("late on the first of a month in UTC it is still the previous month in Ecuador", () => {
  // 03:00 UTC on 1 October is 22:00 on 30 September in Guayaquil, so last
  // month is August, not September.
  const now = new Date("2026-10-01T03:00:00Z");
  assert.deepEqual(previousEcuadorCalendarMonth(now), { from: "2026-08-01", to: "2026-08-31" });
});

test("the year boundary moves in Ecuador time too", () => {
  // 02:00 UTC on 1 January 2027 is still 31 December 2026 in Guayaquil.
  const now = new Date("2027-01-01T02:00:00Z");
  assert.deepEqual(previousEcuadorCalendarMonth(now), { from: "2026-11-01", to: "2026-11-30" });
});

test("once the first of the month has begun in Ecuador, last month is the one that just ended", () => {
  const now = new Date("2026-10-01T05:00:00Z");
  assert.deepEqual(previousEcuadorCalendarMonth(now), { from: "2026-09-01", to: "2026-09-30" });
});

test("February's last day follows the leap year", () => {
  assert.deepEqual(previousEcuadorCalendarMonth(new Date("2028-03-15T12:00:00Z")), {
    from: "2028-02-01",
    to: "2028-02-29",
  });
  assert.deepEqual(previousEcuadorCalendarMonth(new Date("2027-03-15T12:00:00Z")), {
    from: "2027-02-01",
    to: "2027-02-28",
  });
});

test("the list's Emission Date range is the starting range when both ends are set", () => {
  const now = new Date("2026-09-16T15:00:00Z");
  assert.deepEqual(
    startingTaxDocumentArchiveRange({ issuedFrom: "2026-07-01", issuedTo: "2026-09-30" }, now),
    { from: "2026-07-01", to: "2026-09-30" },
  );
});

test("a list range with one end set fills the other end sensibly", () => {
  const now = new Date("2026-09-16T15:00:00Z");
  // "Since July 1st" runs to today in Ecuador.
  assert.deepEqual(startingTaxDocumentArchiveRange({ issuedFrom: "2026-07-01", issuedTo: "" }, now), {
    from: "2026-07-01",
    to: "2026-09-16",
  });
  // "Up to 20 August" starts on the first of that month.
  assert.deepEqual(startingTaxDocumentArchiveRange({ issuedFrom: "", issuedTo: "2026-08-20" }, now), {
    from: "2026-08-01",
    to: "2026-08-20",
  });
});

test("a list range left open at the end uses Ecuador's today, not UTC's", () => {
  const now = new Date("2026-09-17T03:00:00Z");
  assert.deepEqual(startingTaxDocumentArchiveRange({ issuedFrom: "2026-09-01", issuedTo: "" }, now), {
    from: "2026-09-01",
    to: "2026-09-16",
  });
});

test("the download address carries the range as from and to", () => {
  assert.equal(
    taxDocumentArchiveUrl({ from: "2026-08-01", to: "2026-08-31" }),
    "/api/operator/invoicing/archive?from=2026-08-01&to=2026-08-31",
  );
});

test("a range is downloadable only with both ends set and in order", () => {
  assert.equal(isTaxDocumentArchiveRangeComplete({ from: "2026-08-01", to: "2026-08-31" }), true);
  assert.equal(isTaxDocumentArchiveRangeComplete({ from: "2026-08-31", to: "2026-08-31" }), true);
  assert.equal(isTaxDocumentArchiveRangeComplete({ from: "", to: "2026-08-31" }), false);
  assert.equal(isTaxDocumentArchiveRangeComplete({ from: "2026-08-01", to: "" }), false);
  assert.equal(isTaxDocumentArchiveRangeComplete({ from: "2026-09-01", to: "2026-08-31" }), false);
});

test("the dialog has its words in both languages", () => {
  const keys = ["action", "title", "description", "fromLabel", "toLabel", "filtersNote", "download", "cancel", "rangeInverted"];
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8")) as {
      operator: { taxDocumentArchive?: Record<string, string> };
    };
    for (const key of keys) {
      assert.ok(catalog.operator.taxDocumentArchive?.[key], `${locale} is missing operator.taxDocumentArchive.${key}`);
    }
  }
});
