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

import type { InvoiceStatus } from "./operator-api";

/**
 * Whether the downloads card offers the RIDE: yes for an authorized
 * document, no otherwise. The kind is not asked for — that it makes no
 * difference is the rule.
 */
export function offersRide(status: InvoiceStatus): boolean {
  return status === "authorized";
}
