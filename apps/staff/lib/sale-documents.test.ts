import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { DOCUMENT_ROLE_KEYS, documentRoleKey, reissueTrailOf, type DocumentRoleKey } from "./sale-documents.ts";

// WHAT THESE ASSERT. The API orders the chain and names each document's
// role (#486); the page decides which roles earn a badge and under which
// document the reissue's trail is written. The last test keeps the badges
// from reaching an operator with nothing to say in either language.

const reissued = {
  supersedes_invoice_id: "old",
  reissued_by: "operator@example.com",
  reissued_at: "2026-08-26T12:00:00Z",
  reissue_note: "buyer wrote in",
};

test("the current and the superseded factura are badged; a Credit Note and a dead factura are not", () => {
  assert.equal(documentRoleKey("current"), "saleDocumentRoleCurrent");
  assert.equal(documentRoleKey("superseded"), "invoicingSupersededBadge");
  assert.equal(documentRoleKey("credit_note"), null);
  assert.equal(documentRoleKey("not_current"), null);
});

test("the trail is written under the corrected factura, with its note", () => {
  assert.deepEqual(reissueTrailOf(reissued), {
    by: "operator@example.com",
    at: "2026-08-26T12:00:00Z",
    note: "buyer wrote in",
  });
  assert.deepEqual(reissueTrailOf({ ...reissued, reissue_note: null }), {
    by: "operator@example.com",
    at: "2026-08-26T12:00:00Z",
    note: null,
  });
  assert.deepEqual(reissueTrailOf({ ...reissued, reissue_note: "" })?.note, null);
});

// The superseded factura and the reissue's Credit Note carry the same trail
// from the API; the page writes it once, and neither of them is the one.
test("the trail is not repeated under the superseded factura or the Credit Note", () => {
  assert.equal(reissueTrailOf({ ...reissued, supersedes_invoice_id: null }), null);
});

test("a corrected factura whose trail the API left empty gets no sentence", () => {
  assert.equal(reissueTrailOf({ ...reissued, reissued_by: null }), null);
  assert.equal(reissueTrailOf({ ...reissued, reissued_at: null }), null);
});

test("every badge has its words in both languages", () => {
  const keys = Object.values(DOCUMENT_ROLE_KEYS).filter((key): key is DocumentRoleKey => key !== null);
  for (const locale of ["en", "es"]) {
    const catalog = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
    ) as { operator: Record<string, string> };
    for (const key of keys) {
      assert.ok(catalog.operator[key], `${locale} is missing operator.${key}`);
    }
  }
});
