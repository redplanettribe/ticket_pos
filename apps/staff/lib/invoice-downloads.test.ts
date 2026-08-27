import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { rideOffer } from "./invoice-downloads.ts";

// WHAT THESE ASSERT (#494, #495, ADR 0062). The RIDE download is offered
// for an authorized document of every kind and for nothing else: the API
// refuses the PDF for an unauthorized document, and the page must not
// offer what will be refused. The last test keeps the download's words from
// reaching an operator empty in either language.

const kinds = ["manual", "sale", "credit_note"] as const;

const statuses = [
  "owed",
  "pending",
  "not_authorized",
  "rejected",
  "needs_attention",
  "withdrawn",
  "annulled",
] as const;

test("an authorized document of every kind offers the PDF download", () => {
  for (const kind of kinds) {
    assert.equal(rideOffer({ status: "authorized", kind }), "download", kind);
  }
});

test("a document in any other status offers no RIDE at all", () => {
  for (const kind of kinds) {
    for (const status of statuses) {
      assert.equal(rideOffer({ status, kind }), null, `${kind} ${status}`);
    }
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
