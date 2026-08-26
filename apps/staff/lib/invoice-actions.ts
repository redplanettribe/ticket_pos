/**
 * What the document detail offers a Platform Operator, decided once and
 * tested under `node --test` (#477, ADR 0060).
 *
 * The API is the gate: Check status and Resend are refused on an authorized
 * or annulled document, Mark annulled outside `pending` and
 * `needs_attention`, and every refusal is shown by code. This exists so the
 * page never renders a lever the API is certain to refuse — a greyed button
 * invites the question of why — and so the one rule lives in one place.
 */

import type { InvoiceStatus } from "./operator-api";

/**
 * Which levers the actions card shows for a document in this state.
 *
 * `check` and `resend` go together: both are the operator's remedies for a
 * document the SRI has not authorized. `annul` is the operator's record of a
 * manual portal act, offered only where they could have made one — a
 * document the SRI holds (pending) or refused / lost (needs_attention).
 * An authorized document is a legal artifact; an annulled or withdrawn one
 * is finished; an owed one has not been signed, so there is nothing at the
 * SRI to check, resend or annul.
 */
export type InvoiceLevers = {
  check: boolean;
  resend: boolean;
  annul: boolean;
};

export function invoiceLevers(status: InvoiceStatus, signed: boolean): InvoiceLevers {
  if (!signed) {
    return { check: false, resend: false, annul: false };
  }
  switch (status) {
    case "pending":
    case "needs_attention":
      return { check: true, resend: true, annul: true };
    case "not_authorized":
    case "rejected":
      return { check: true, resend: true, annul: false };
    default:
      return { check: false, resend: false, annul: false };
  }
}

/** Whether the actions card is rendered at all: only when some lever is. */
export function hasInvoiceLevers(levers: InvoiceLevers): boolean {
  return levers.check || levers.resend || levers.annul;
}
