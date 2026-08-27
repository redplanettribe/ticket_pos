/**
 * The Sale lookup's documents (#486, ADR 0061): what the page decides about
 * a document the API has already placed in its sale's chain, tested under
 * `node --test`.
 *
 * The API decides the ORDER and the ROLE — superseded factura, its Credit
 * Note, the corrected factura, the current one marked — and reads the
 * reissue's trail beside every document the reissue concerns. What is left
 * to the page is which roles earn a badge beside the kind and the status,
 * and beside which document the trail is written out: once, under the
 * corrected factura, the one the reissue produced.
 */

import type { OperatorDocumentRole, OperatorSaleDocument } from "./operator-api";

/**
 * The `operator` catalog key a role is badged with, or null where the row's
 * kind and status already say it: a Credit Note is badged as its kind, and
 * a withdrawn or annulled factura as its status.
 */
export const DOCUMENT_ROLE_KEYS = {
  current: "saleDocumentRoleCurrent",
  superseded: "invoicingSupersededBadge",
  credit_note: null,
  not_current: null,
} as const satisfies Record<OperatorDocumentRole, string | null>;

export type DocumentRoleKey = Exclude<(typeof DOCUMENT_ROLE_KEYS)[OperatorDocumentRole], null>;

export function documentRoleKey(role: OperatorDocumentRole): DocumentRoleKey | null {
  return DOCUMENT_ROLE_KEYS[role] ?? null;
}

export type ReissueTrail = { by: string; at: string; note: string | null };

/**
 * The reissue's who, when and note to write under this document — the
 * corrected factura's own, and nobody else's: the superseded factura and
 * the Credit Note carry the same trail and point at the corrected one from
 * their details, and one sentence per reissue is enough on a page that
 * lists the whole chain. Null on a document no reissue produced, or whose
 * trail the API left empty.
 */
export function reissueTrailOf(
  document: Pick<OperatorSaleDocument, "supersedes_invoice_id" | "reissued_by" | "reissued_at" | "reissue_note">,
): ReissueTrail | null {
  if (!document.supersedes_invoice_id || !document.reissued_by || !document.reissued_at) {
    return null;
  }
  return { by: document.reissued_by, at: document.reissued_at, note: document.reissue_note || null };
}
