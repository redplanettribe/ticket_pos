import assert from "node:assert/strict";
import test from "node:test";

import {
  hasSaleDocuments,
  saleDocumentDownloadHref,
  saleDocumentLabelKey,
  saleDocumentPendingKey,
  type SaleDocument,
} from "./sale-documents.ts";

const SALE_ID = "11111111-2222-3333-4444-555555555555";
const DOC_ID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";

function authorized(kind = "sale"): SaleDocument {
  return {
    id: DOC_ID,
    kind,
    status: "authorized",
    download_url: `/api/v1/customer/ticket-sales/${SALE_ID}/tax-documents/${DOC_ID}/xml`,
  };
}

function onItsWay(kind = "sale"): SaleDocument {
  return { id: DOC_ID, kind, status: "on_its_way", download_url: null };
}

// The download goes through this app's own relay, never to the API's path the
// payload names (ADR 0008).
test("saleDocumentDownloadHref points at the BFF relay once authorized", () => {
  assert.equal(
    saleDocumentDownloadHref(SALE_ID, authorized()),
    `/api/customer/ticket-sales/${SALE_ID}/tax-documents/${DOC_ID}/xml`,
  );
});

test("saleDocumentDownloadHref offers nothing while the document is on its way", () => {
  assert.equal(saleDocumentDownloadHref(SALE_ID, onItsWay()), null);
  // Authorized in word but with no path from the API: nothing to point at.
  assert.equal(
    saleDocumentDownloadHref(SALE_ID, { ...authorized(), download_url: null }),
    null,
  );
});

test("saleDocumentLabelKey names each kind by its own word", () => {
  assert.equal(saleDocumentLabelKey(authorized("sale")), "invoice");
  assert.equal(saleDocumentLabelKey(authorized("credit_note")), "creditNote");
  // An unknown kind is a factura rather than a hole in the card.
  assert.equal(saleDocumentLabelKey(authorized("something_new")), "invoice");
});

// The sentence and the download are never drawn together.
test("saleDocumentPendingKey speaks only before authorization", () => {
  assert.equal(saleDocumentPendingKey(onItsWay()), "onItsWay");
  assert.equal(saleDocumentPendingKey(onItsWay("credit_note")), "creditNoteOnItsWay");
  assert.equal(saleDocumentPendingKey(authorized()), null);
});

// A Sale that owes nothing draws nothing: the block exists only on House sales.
test("hasSaleDocuments is false while loading and for a sale that owes none", () => {
  assert.equal(hasSaleDocuments(null), false);
  assert.equal(hasSaleDocuments([]), false);
  assert.equal(hasSaleDocuments([onItsWay()]), true);
});
