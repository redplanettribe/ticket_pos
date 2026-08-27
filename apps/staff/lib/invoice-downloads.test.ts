import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { rideOffer } from "./invoice-downloads.ts";

// WHAT THESE ASSERT (#494, ADR 0062). The RIDE download is offered for an
// authorized factura and for nothing else: the API refuses the PDF for an
// unauthorized document, and the page must not offer what will be refused.
// The Credit Note keeps its print view until #495. The last test keeps the
// download's words from reaching an operator empty in either language.

const statuses = [
  "owed",
  "pending",
  "not_authorized",
  "rejected",
  "needs_attention",
  "withdrawn",
  "annulled",
] as const;

test("an authorized manual Tax Invoice and Sale Invoice offer the PDF download", () => {
  assert.equal(rideOffer({ status: "authorized", kind: "manual" }), "download");
  assert.equal(rideOffer({ status: "authorized", kind: "sale" }), "download");
});

test("a factura in any other status offers no RIDE at all", () => {
  for (const status of statuses) {
    assert.equal(rideOffer({ status, kind: "manual" }), null, `manual ${status}`);
    assert.equal(rideOffer({ status, kind: "sale" }), null, `sale ${status}`);
  }
});

test("a Credit Note keeps the print view until #495, whatever its status", () => {
  assert.equal(rideOffer({ status: "authorized", kind: "credit_note" }), "print_view");
  assert.equal(rideOffer({ status: "pending", kind: "credit_note" }), "print_view");
});

test("the download has its words in both languages", () => {
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: Record<string, string> };
    assert.ok(catalog.operator.invoicingDownloadRide, `${locale} is missing operator.invoicingDownloadRide`);
  }
});
