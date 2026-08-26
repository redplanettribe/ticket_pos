/**
 * The Tax Documents of one Ticket Sale as the Customer Area shows them (#475,
 * ADR 0060): the factura a paid House sale owes, and the nota de crédito its
 * reversal will owe, as rules rather than as markup.
 *
 * WHAT THE BUYER IS TOLD IS TWO THINGS. The API already speaks in the buyer's
 * words — `authorized` or `on_its_way`, never a state the SRI named and never
 * a message it sent — so nothing here maps or softens anything. It decides
 * only where a download is offered and which word names each document.
 *
 * Pure and dependency-free — no React, no i18n runtime — so it is directly
 * unit-testable, as its siblings are. It returns SHAPES and never a sentence.
 */

/** One document as the API lists it on a Sale. */
export type SaleDocument = {
  id: string;
  /** `sale` is a factura; `credit_note` a nota de crédito. */
  kind: string;
  /** `authorized`, or `on_its_way` for everything before that. */
  status: string;
  /** The API's own download path once authorized, null before. Not the path
   * this app fetches — a browser never addresses the Go API (ADR 0008) — but
   * the fact that decides whether a download is drawn. */
  download_url: string | null;
};

/**
 * saleDocumentDownloadHref is where the browser downloads one document from:
 * this app's own BFF relay, and only once the API offers a download.
 *
 * Built from the two ids rather than from `download_url`, because the API's
 * path names the API and the browser must not follow it there. The relay
 * forwards under the session cookie and hands the XML back as a file.
 */
export function saleDocumentDownloadHref(
  ticketSaleId: string,
  document: SaleDocument,
): string | null {
  if (document.status !== "authorized" || !document.download_url) {
    return null;
  }
  return `/api/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tax-documents/${encodeURIComponent(document.id)}/xml`;
}

/**
 * saleDocumentLabelKey names the message that says what a document IS, under
 * the `customerArea.documents` namespace. A kind this app does not know reads
 * as a factura: the platform issues nothing else to a buyer today, and a new
 * kind must be named here before it is drawn under the wrong word.
 */
export function saleDocumentLabelKey(document: SaleDocument): "invoice" | "creditNote" {
  return document.kind === "credit_note" ? "creditNote" : "invoice";
}

/**
 * saleDocumentPendingKey names the "on its way" sentence for a document not
 * yet authorized, or null once it is — the sentence and the download are
 * never drawn together.
 */
export function saleDocumentPendingKey(
  document: SaleDocument,
): "onItsWay" | "creditNoteOnItsWay" | null {
  if (document.status === "authorized") {
    return null;
  }
  return document.kind === "credit_note" ? "creditNoteOnItsWay" : "onItsWay";
}

/**
 * hasSaleDocuments says whether the block is drawn at all: a Sale that owes
 * nothing — a free, imported or non-House sale, which is nearly every sale —
 * lists no document and the card is exactly the card it was.
 */
export function hasSaleDocuments(documents: SaleDocument[] | null): documents is SaleDocument[] {
  return documents !== null && documents.length > 0;
}
