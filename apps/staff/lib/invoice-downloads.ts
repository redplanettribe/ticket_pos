/**
 * Which RIDE a Tax Invoice's detail offers (#494, ADR 0062), tested under
 * `node --test`.
 *
 * The API renders the RIDE as a PDF and refuses it for anything not
 * authorized — a RIDE without the número de autorización has no validity —
 * so the page offers the download only once the document is authorized,
 * and shows nothing in its place before that: an unauthorized document has
 * no RIDE to hand over by mistake. The factura kinds (`manual`, `sale`) use
 * the PDF now; the Credit Note keeps the React print view until #495 gives
 * it its own layout and deletes the print view for good.
 */

import type { InvoiceKind, InvoiceStatus } from "./operator-api";

export type RideOffer = "download" | "print_view" | null;

const PDF_KINDS: readonly InvoiceKind[] = ["manual", "sale"];

/**
 * What the downloads card offers for the RIDE: the PDF download for an
 * authorized factura, the print-view link for a Credit Note (until #495),
 * nothing for an unauthorized factura.
 */
export function rideOffer(invoice: { status: InvoiceStatus; kind: InvoiceKind }): RideOffer {
  if (!PDF_KINDS.includes(invoice.kind)) {
    return "print_view";
  }
  return invoice.status === "authorized" ? "download" : null;
}
