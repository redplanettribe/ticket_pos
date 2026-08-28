import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { offersRide } from "./invoice-downloads.ts";

// WHAT THESE ASSERT (#494, #495, ADR 0062). The RIDE download is offered
// for an authorized document, whatever its kind, and for nothing else: the
// API refuses the PDF for an unauthorized document, and the page must not
// offer what will be refused. The rule reads the status alone — that the
// kind makes no difference is the rule. The last test keeps the download's
// words from reaching an operator empty in either language.

const otherStatuses = [
  "owed",
  "pending",
  "not_authorized",
  "rejected",
  "needs_attention",
  "withdrawn",
  "annulled",
] as const;

test("an authorized document offers the PDF download", () => {
  assert.equal(offersRide("authorized"), true);
});

test("a document in any other status offers no RIDE at all", () => {
  for (const status of otherStatuses) {
    assert.equal(offersRide(status), false, status);
  }
});

test("the download has its words in both languages", () => {
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: Record<string, string> };
    assert.ok(catalog.operator.invoicingDownloadRide, `${locale} is missing operator.invoicingDownloadRide`);
  }
});
