import assert from "node:assert/strict";
import test from "node:test";

import {
  hasSaleDocuments,
  orderSaleDocuments,
  saleDocumentBadgeKey,
  saleDocumentDownloadHref,
  saleDocumentLabelKey,
  saleDocumentPendingKey,
  saleDocumentRideHref,
  saleDocumentRole,
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
    ride_url: `/api/v1/customer/ticket-sales/${SALE_ID}/tax-documents/${DOC_ID}/ride`,
  };
}

function onItsWay(kind = "sale"): SaleDocument {
  return { id: DOC_ID, kind, status: "on_its_way", download_url: null, ride_url: null };
}

// The RIDE beside the XML (#497, ADR 0062): the sibling relay, offered exactly
// when the API offers it.
test("saleDocumentRideHref points at the BFF relay once authorized", () => {
  assert.equal(
    saleDocumentRideHref(SALE_ID, authorized()),
    `/api/customer/ticket-sales/${SALE_ID}/tax-documents/${DOC_ID}/ride`,
  );
  // A credit note's RIDE is offered as a factura's is.
  assert.equal(
    saleDocumentRideHref(SALE_ID, authorized("credit_note")),
    `/api/customer/ticket-sales/${SALE_ID}/tax-documents/${DOC_ID}/ride`,
  );
});

test("saleDocumentRideHref offers nothing while the document is on its way", () => {
  assert.equal(saleDocumentRideHref(SALE_ID, onItsWay()), null);
  assert.equal(saleDocumentRideHref(SALE_ID, onItsWay("credit_note")), null);
  // Authorized in word but with no RIDE path from the API — a payload from
  // before the RIDE existed: the XML alone, never a link to a 404.
  assert.equal(saleDocumentRideHref(SALE_ID, { ...authorized(), ride_url: null }), null);
  const preRide: SaleDocument = {
    id: DOC_ID,
    kind: "sale",
    status: "authorized",
    download_url: `/api/v1/customer/ticket-sales/${SALE_ID}/tax-documents/${DOC_ID}/xml`,
  };
  assert.equal(saleDocumentRideHref(SALE_ID, preRide), null);
  assert.notEqual(saleDocumentDownloadHref(SALE_ID, preRide), null);
});

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

// The chain after a Sale Invoice Reissue (#485, ADR 0061).

const OLD = "00000000-0000-0000-0000-000000000001";
const NOTE = "00000000-0000-0000-0000-000000000002";
const NEW = "00000000-0000-0000-0000-000000000003";
const NOTE_2 = "00000000-0000-0000-0000-000000000004";
const NEW_2 = "00000000-0000-0000-0000-000000000005";

function chained(
  id: string,
  kind: string,
  role: string,
  links: Partial<
    Pick<SaleDocument, "supersedes_invoice_id" | "superseded_by_invoice_id" | "credits_invoice_id">
  > = {},
  status = "authorized",
): SaleDocument {
  return {
    id,
    kind,
    status,
    download_url:
      status === "authorized" ? `/api/v1/customer/ticket-sales/${SALE_ID}/tax-documents/${id}/xml` : null,
    ride_url:
      status === "authorized" ? `/api/v1/customer/ticket-sales/${SALE_ID}/tax-documents/${id}/ride` : null,
    role,
    supersedes_invoice_id: null,
    superseded_by_invoice_id: null,
    credits_invoice_id: null,
    ...links,
  };
}

// As the API lists them: in the order they came to exist.
function reissueChain(): SaleDocument[] {
  return [
    chained(OLD, "sale", "superseded", { superseded_by_invoice_id: NEW }),
    chained(NOTE, "credit_note", "credit_note", { credits_invoice_id: OLD }, "on_its_way"),
    chained(NEW, "sale", "current", { supersedes_invoice_id: OLD }, "on_its_way"),
  ];
}

test("saleDocumentRole reads the API's word and fills a payload that has none", () => {
  assert.equal(saleDocumentRole(chained(OLD, "sale", "superseded")), "superseded");
  assert.equal(saleDocumentRole(chained(NEW, "sale", "current")), "current");
  assert.equal(saleDocumentRole(chained(NOTE, "credit_note", "credit_note")), "credit_note");
  // Before the chain existed: a factura is the current one, a credit note is one.
  assert.equal(saleDocumentRole(authorized("sale")), "current");
  assert.equal(saleDocumentRole(authorized("credit_note")), "credit_note");
  // A role this app does not know falls back on the kind rather than on a hole.
  assert.equal(saleDocumentRole(chained(OLD, "sale", "something_new")), "current");
});

test("orderSaleDocuments puts the current factura first, then the credit note, then the superseded one", () => {
  assert.deepEqual(
    orderSaleDocuments(reissueChain()).map((d) => d.id),
    [NEW, NOTE, OLD],
  );
});

test("orderSaleDocuments keeps the API's order within a role over two reissues", () => {
  const chain = [
    chained(OLD, "sale", "superseded", { superseded_by_invoice_id: NEW }),
    chained(NOTE, "credit_note", "credit_note", { credits_invoice_id: OLD }),
    chained(NEW, "sale", "superseded", { supersedes_invoice_id: OLD, superseded_by_invoice_id: NEW_2 }),
    chained(NOTE_2, "credit_note", "credit_note", { credits_invoice_id: NEW }),
    chained(NEW_2, "sale", "current", { supersedes_invoice_id: NEW }, "on_its_way"),
  ];
  assert.deepEqual(
    orderSaleDocuments(chain).map((d) => d.id),
    [NEW_2, NOTE, NOTE_2, OLD, NEW],
  );
});

test("orderSaleDocuments leaves a sale with no chain exactly as listed", () => {
  const plain = [authorized("sale")];
  assert.deepEqual(orderSaleDocuments(plain), plain);
  // A reversal's factura and credit note, no reissue: the factura stays first.
  const reversed = [
    chained(OLD, "sale", "current", { }),
    chained(NOTE, "credit_note", "credit_note", { credits_invoice_id: OLD }),
  ];
  assert.deepEqual(orderSaleDocuments(reversed).map((d) => d.id), [OLD, NOTE]);
  assert.deepEqual(orderSaleDocuments([]), []);
});

test("orderSaleDocuments does not reorder the list it was given", () => {
  const chain = reissueChain();
  const ids = chain.map((d) => d.id);
  orderSaleDocuments(chain);
  assert.deepEqual(chain.map((d) => d.id), ids);
});

test("saleDocumentBadgeKey marks a superseded factura and nothing else", () => {
  const [old, note, current] = reissueChain();
  assert.equal(saleDocumentBadgeKey(old), "superseded");
  assert.equal(saleDocumentBadgeKey(note), null);
  assert.equal(saleDocumentBadgeKey(current), null);
  assert.equal(saleDocumentBadgeKey(authorized("sale")), null);
});

// The superseded factura is still labelled a factura, still downloadable, and
// never told to be "on its way": it was delivered.
test("a superseded factura keeps its label and its download", () => {
  const [old] = reissueChain();
  assert.equal(saleDocumentLabelKey(old), "invoice");
  assert.equal(
    saleDocumentDownloadHref(SALE_ID, old),
    `/api/customer/ticket-sales/${SALE_ID}/tax-documents/${OLD}/xml`,
  );
  assert.equal(saleDocumentPendingKey(old), null);
});
