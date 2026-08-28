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
 * AFTER A SALE INVOICE REISSUE THE SALE HAS A CHAIN (#485, ADR 0061): a
 * corrected factura, the nota de crédito that cancelled the earlier one, and
 * the earlier one itself — still authorized, still the buyer's, no longer
 * current. The API says which is which as a `role`; this module orders the
 * chain — the current factura first, then the credit notes, then the
 * superseded facturas — and names the label a superseded one carries. The
 * superseded XML stays downloadable: it is a legal document the buyer received.
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
  /** The API's path of the RIDE (PDF) once authorized, null before (#497, ADR
   * 0062); set exactly when `download_url` is. Optional so a payload from
   * before the RIDE existed still reads: absent, no RIDE is drawn. */
  ride_url?: string | null;
  /** `current` for the factura that stands, `superseded` for one a reissue
   * corrected, `credit_note`. Optional so a payload from before the chain
   * existed still reads: absent, a factura is current. */
  role?: string;
  /** The ids a chained document points at, each another document of the same
   * list or null: the factura this one corrects, the one that corrects this
   * one, the factura a credit note credits. */
  supersedes_invoice_id?: string | null;
  superseded_by_invoice_id?: string | null;
  credits_invoice_id?: string | null;
};

/** The role a document plays, with the pre-chain payload's silence filled. */
export type SaleDocumentRole = "current" | "superseded" | "credit_note";

/**
 * saleDocumentRole reads the API's role, and for a payload that carries none
 * — or a word this app does not know — falls back on the kind: a credit note
 * is a credit note and any other document is the current factura, which is
 * what every Sale had before a reissue could exist.
 */
export function saleDocumentRole(document: SaleDocument): SaleDocumentRole {
  if (document.role === "superseded" || document.role === "credit_note" || document.role === "current") {
    return document.role;
  }
  return document.kind === "credit_note" ? "credit_note" : "current";
}

const ROLE_ORDER: Record<SaleDocumentRole, number> = {
  current: 0,
  credit_note: 1,
  superseded: 2,
};

/**
 * orderSaleDocuments puts the chain in the order the buyer reads it: the
 * current factura first, then the credit note(s), then the superseded
 * factura(s). Within a role the API's own order — oldest first — is kept, so
 * a chain of two reissues reads the same way twice. A Sale with one factura
 * is unchanged: one document has one order.
 */
export function orderSaleDocuments(documents: SaleDocument[]): SaleDocument[] {
  return documents
    .map((document, index) => ({ document, index }))
    .sort(
      (a, b) =>
        ROLE_ORDER[saleDocumentRole(a.document)] - ROLE_ORDER[saleDocumentRole(b.document)] ||
        a.index - b.index,
    )
    .map(({ document }) => document);
}

/**
 * saleDocumentBadgeKey names the message drawn beside a document's label, or
 * null when it carries none: only a superseded factura is marked, so the
 * buyer holding two facturas knows which one stands. A credit note and the
 * current factura are labelled by their kind alone.
 */
export function saleDocumentBadgeKey(document: SaleDocument): "superseded" | null {
  return saleDocumentRole(document) === "superseded" ? "superseded" : null;
}

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
 * saleDocumentRideHref is where the browser downloads one document's RIDE
 * from (#497, ADR 0062): the sibling BFF relay, and only once the API offers
 * the RIDE — which it does for every authorized document, `current`,
 * `superseded` and `credit_note` alike, and for none before. Built from the
 * ids for the reason saleDocumentDownloadHref is; `ride_url` is read only as
 * the fact that a RIDE is offered, so a payload from before the RIDE existed
 * draws the XML alone rather than a link to a 404.
 */
export function saleDocumentRideHref(
  ticketSaleId: string,
  document: SaleDocument,
): string | null {
  if (document.status !== "authorized" || !document.ride_url) {
    return null;
  }
  return `/api/customer/ticket-sales/${encodeURIComponent(ticketSaleId)}/tax-documents/${encodeURIComponent(document.id)}/ride`;
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
