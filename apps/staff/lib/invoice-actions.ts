/**
 * What the document detail offers a Platform Operator, decided once and
 * tested under `node --test` (#477, ADR 0060; #483, ADR 0061).
 *
 * The API is the gate: Check status and Resend are refused on an authorized
 * or annulled document, Mark annulled outside `pending` and
 * `needs_attention`, Reissue anywhere but on a current, authorized Sale
 * Invoice, and every refusal is shown by code. This exists so the page never
 * renders a lever the API is certain to refuse — a greyed button invites the
 * question of why — and so the one rule lives in one place.
 */

import type { InvoiceKind, InvoiceStatus } from "./operator-api";

/**
 * Which levers the detail shows for a document in this state.
 *
 * `check` and `resend` go together: both are the operator's remedies for a
 * document the SRI has not authorized. `annul` is the operator's record of a
 * manual portal act, offered only where they could have made one — a
 * document the SRI holds (pending) or refused / lost (needs_attention).
 * An authorized document is a legal artifact; an annulled or withdrawn one
 * is finished; an owed one has not been signed, so there is nothing at the
 * SRI to check, resend or annul.
 *
 * `resend` alone falls away on a document the SRI refuses by NUMBER (#577,
 * ADR 0068). Resend sends the same clave and the same secuencial, which is
 * the whole of what error 45 objects to, so the API refuses it outright with
 * INVOICE_REFUSED_BY_NUMBER; `check` stays, because asking the authority what
 * it holds sends nothing. The page must SAY SO where the button was — this is
 * the one lever whose absence is not self-explanatory from the status, and an
 * operator who has been resending for two days is owed the reason rather than
 * a silently shorter row of buttons.
 *
 * `reissue` is the one lever an AUTHORIZED document has (#483): a Sale
 * Invoice Reissue, offered on a Sale Invoice that is the Sale's current one
 * — authorized, not superseded by a corrected factura, and not credited by
 * any live Credit Note, which is what a reversal, a reissue in flight and a
 * settled reissue all leave behind (a reissue that died leaves none, #484).
 * A manual Tax Invoice is issued again by hand and a Credit Note is never
 * credited, so neither is offered it.
 */
export type InvoiceLevers = {
  check: boolean;
  resend: boolean;
  annul: boolean;
  reissue: boolean;
};

/**
 * The chain facts the reissue lever is decided on, beside the status:
 * absent (the default) means "not a Sale Invoice", and no reissue.
 */
export type InvoiceChainFacts = {
  kind: InvoiceKind;
  superseded_by_invoice_id: string | null;
  credited_by_invoice_id: string | null;
};

const NOT_A_SALE_INVOICE: InvoiceChainFacts = {
  kind: "manual",
  superseded_by_invoice_id: null,
  credited_by_invoice_id: null,
};

export function invoiceLevers(
  status: InvoiceStatus,
  signed: boolean,
  chain: InvoiceChainFacts = NOT_A_SALE_INVOICE,
  refusedByNumber = false,
): InvoiceLevers {
  const reissue = reissueOffered(status, chain);
  if (!signed) {
    return { check: false, resend: false, annul: false, reissue: false };
  }
  // The refusal by number takes `resend` away wherever it was offered, and
  // touches nothing else: Check still asks, Mark annulled is still the
  // operator's record of a portal act (#578 narrows that one, not this).
  const resend = !refusedByNumber;
  switch (status) {
    case "pending":
    case "needs_attention":
      return { check: true, resend, annul: true, reissue };
    case "not_authorized":
    case "rejected":
      return { check: true, resend, annul: false, reissue };
    default:
      return { check: false, resend: false, annul: false, reissue };
  }
}

/**
 * Whether Reissue is offered: exactly where the API would allow it. The
 * detail's `credited_by_invoice_id` names a LIVE Credit Note only (#484):
 * one that died — the reissue's, refused by the SRI and marked annulled —
 * credits nothing, the corrected factura was withdrawn with it, and the
 * factura is current again with the link null, so the lever is back.
 */
function reissueOffered(status: InvoiceStatus, chain: InvoiceChainFacts): boolean {
  return (
    chain.kind === "sale" &&
    status === "authorized" &&
    chain.superseded_by_invoice_id === null &&
    chain.credited_by_invoice_id === null
  );
}

/**
 * Whether the SRI-remedies card is rendered at all: only when some remedy
 * is. Reissue is its own card, rendered on `levers.reissue` alone.
 */
export function hasInvoiceLevers(levers: InvoiceLevers): boolean {
  return levers.check || levers.resend || levers.annul;
}
