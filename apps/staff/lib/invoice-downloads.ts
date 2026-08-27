/**
 * Whether a Tax Invoice's detail offers its RIDE (#494, #495, ADR 0062),
 * tested under `node --test`.
 *
 * The API renders the RIDE as a PDF and refuses it for anything not
 * authorized — a RIDE without the número de autorización has no validity —
 * so the page offers the download only once the document is authorized,
 * and shows nothing in its place before that: an unauthorized document has
 * no RIDE to hand over by mistake. Every kind — manual Tax Invoice, Sale
 * Invoice, Credit Note — is the same PDF from the same endpoint; there is
 * one RIDE (ADR 0062 §4), and the print view that once stood in for the
 * Credit Note's is gone.
 */

import type { InvoiceKind, InvoiceStatus } from "./operator-api";

export type RideOffer = "download" | null;

/**
 * What the downloads card offers for the RIDE: the PDF download for an
 * authorized document, nothing otherwise. The kind is taken and not read —
 * that it makes no difference is the rule.
 */
export function rideOffer(invoice: { status: InvoiceStatus; kind: InvoiceKind }): RideOffer {
  return invoice.status === "authorized" ? "download" : null;
}
