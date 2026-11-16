/**
 * What the document detail offers a Platform Operator, decided once and
 * tested under `node --test` (#477, ADR 0060; #483, ADR 0061).
 *
 * The API is the gate: Check status and Resend are refused on an authorized
 * or annulled document, a Credit Note's Mark annulled outside `pending` and
 * `needs_attention`, Reissue anywhere but on a current, authorized Sale
 * Invoice, and every refusal is shown by code. This exists so the page never
 * renders a lever the API is certain to refuse — a greyed button invites the
 * question of why — and so the one rule lives in one place.
 */

import type { InvoiceKind, InvoiceStatus, OperatorInvoiceAttempt } from "./operator-api";

/**
 * Which levers the detail shows for a document in this state.
 *
 * `check` and `resend` go together: both are the operator's remedies for a
 * document the SRI has not authorized. `annul` is the operator's record of a
 * manual portal act, offered only where they could have made one — a
 * document the SRI holds (pending) or refused / lost (needs_attention) - and
 * ON A CREDIT NOTE ALONE since #745 (spec #742, ADR 0078). A factura's Mark
 * annulled moved to the annulment panel at the top of its page
 * (annulment-panel.ts), which admits it authorized too and takes the typed
 * number; the card keeps the Credit Note's plain confirm exactly as it was.
 * An authorized document is a legal artifact; an annulled or withdrawn one
 * is finished; an owed one has not been signed, so there is nothing at the
 * SRI to check, resend or annul.
 *
 * `abandon` REPLACES `annul` on a document the SRI refuses by NUMBER (#578,
 * ADR 0068), and the swap is the point rather than a detail. Mark annulled
 * records an annulment the operator performed by hand at the SRI portal;
 * for a number the SRI never took, the portal shows nothing under it, so
 * there is nothing there to have been annulled and the API now refuses the
 * press with INVOICE_ABANDON_INSTEAD. Abandon is the honest act in its
 * place: it records that the SRI never took the document and never will.
 * The two make opposite claims about the SRI and are never offered together.
 *
 * Abandon is offered only from the three states a refusal leaves a document
 * in — `needs_attention` for a Sale Invoice, `rejected` and `not_authorized`
 * for a manual one, whose refusals are recorded rather than parked — of any
 * kind, since the number is dead whoever typed the document. Not from
 * `pending`: the SRI still has the document and it is checked, not given up
 * on. The API asks for one thing more that this does not judge, two Check
 * status answers of "nothing known", at least a minute apart, immediately
 * beforehand, and answers INVOICE_CHECK_NOT_FRESH when they are not there;
 * that is a refusal with a next step - Check status twice, at least a minute
 * apart, then Abandon - rather than a lever that should not be drawn.
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
 * `issueAgain` is the one lever an ABANDONED Sale Invoice has (#580,
 * ADR 0068), and the counterpart to `reissue` at the other end of a
 * document's life. Abandon and Mark annulled both leave a Ticket Sale with
 * no factura and, until this, nothing that would ever owe it another; Issue
 * again owes a fresh one, with the original lines, amounts and Recipient,
 * linked to the document it replaces. It is offered on a Sale Invoice that
 * is `abandoned` and has no live replacement; an `annulled` one is offered
 * it by the annulment panel's follow-up chooser instead (#747) - a replacement
 * that itself died leaves the link null (#579), so the chain grows another
 * hop rather than stopping. `withdrawn` is the third death and is
 * deliberately not offered it: such a document was never sent because its
 * Sale was reversed or the Credit Note it followed died, and neither Sale
 * is owed a fresh factura. A manual Tax Invoice is typed again by hand and
 * a Credit Note is never re-owed, so neither is offered it either.
 *
 * The API asks for one thing more that this cannot see - the Ticket Sale
 * must still stand - and answers INVOICE_SALE_REVERSED otherwise; that is a
 * refusal the card shows rather than a lever it withholds, since the page
 * has no reversal to read.
 *
 * `reissue` is the one lever an AUTHORIZED document has (#483): a Sale
 * Invoice Reissue, offered on a Sale Invoice that is the Sale's current one
 * - authorized, not superseded by a corrected factura, and not credited by
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
  abandon: boolean;
  issueAgain: boolean;
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
  const issueAgain = issueAgainOffered(status, chain);
  if (!signed) {
    // An abandoned or annulled document is always signed — both acts refuse
    // an unsigned one — so nothing that reaches here is issued again.
    return { check: false, resend: false, annul: false, reissue: false, abandon: false, issueAgain: false };
  }
  // The refusal by number takes `resend` away wherever it was offered (#577)
  // and hands `annul` to `abandon` (#578). Check still asks the SRI what it
  // holds, and its answer is what an Abandon rests on. A factura's `annul` is
  // the annulment panel's, never this card's (#745).
  const resend = !refusedByNumber;
  const annul = !refusedByNumber && chain.kind === "credit_note";
  switch (status) {
    case "pending":
      // Still with the SRI: checked, not given up on. Mark annulled goes all
      // the same where the number is refused — the portal has nothing to
      // have been annulled — and this is the one state that offers neither
      // it nor Abandon.
      return { check: true, resend, annul, reissue, abandon: false, issueAgain };
    case "needs_attention":
      return { check: true, resend, annul, reissue, abandon: refusedByNumber, issueAgain };
    case "not_authorized":
    case "rejected":
      return { check: true, resend, annul: false, reissue, abandon: refusedByNumber, issueAgain };
    default:
      // Where `abandoned` and `annulled` land, and so the only branch in
      // which `issueAgain` is ever true (on `abandoned` alone, #747).
      return { check: false, resend: false, annul: false, reissue, abandon: false, issueAgain };
  }
}

/**
 * Whether Issue again is offered as its own card: exactly where the API would
 * allow it, minus the one fact the page cannot see (the Ticket Sale still
 * standing). `superseded_by_invoice_id` names a LIVE replacement only
 * (#579): one that died - withdrawn, annulled or abandoned in its turn -
 * leaves the link null, and the dead document may be issued again for a
 * second time.
 *
 * ON AN ABANDONED DOCUMENT ONLY since #747 (spec #742, ADR 0078). An annulled
 * factura's follow-ups - Issue again, Reissue, Reverse the Sale - are the
 * annulment panel's chooser (annulment-panel.ts followUpChooser), which reads
 * the API's own gates, the Annulment Check's mismatch and the Sale's reversal
 * included, so a second Issue again card here would only repeat it.
 */
function issueAgainOffered(status: InvoiceStatus, chain: InvoiceChainFacts): boolean {
  return chain.kind === "sale" && status === "abandoned" && chain.superseded_by_invoice_id === null;
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
  return levers.check || levers.resend || levers.annul || levers.abandon;
}

/**
 * When the SRI gave the answer the resend hint reports (#728): the last
 * attempt's start, which is the query answered `unknown` that turned the
 * hint on. Null when the hint is off.
 *
 * The hint says only what the SRI answered and when, never that the document
 * was not received: the SRI's status services answer "nothing under this
 * clave" for documents they hold (#722), and production factura 225 was
 * told "never received" on one such answer while the SRI held it authorized.
 * Resending is safe either way - a clave the SRI holds is answered as
 * already registered (43/70), never duplicated.
 */
export function resendHintAskedAt(invoice: {
  resend_hint: boolean;
  attempts: readonly { started_at: string }[];
}): string | null {
  if (!invoice.resend_hint || invoice.attempts.length === 0) {
    return null;
  }
  return invoice.attempts[invoice.attempts.length - 1].started_at;
}

/** The toast a Check status or Resend press ends on: its catalog key and how loudly it is shown. */
export type PressToast = {
  key: (typeof PRESS_TOASTS)[keyof typeof PRESS_TOASTS]["key"] | "invoicingCheckedUnrecorded";
  tone: "success" | "info" | "warning";
};

const PRESS_TOASTS = {
  authorized: { key: "invoicingCheckedAuthorized", tone: "success" },
  not_authorized: { key: "invoicingCheckedNotAuthorized", tone: "info" },
  rejected: { key: "invoicingCheckedRejected", tone: "info" },
  received: { key: "invoicingCheckedReceived", tone: "info" },
  unknown: { key: "invoicingCheckedUnknown", tone: "info" },
  error: { key: "invoicingCheckedError", tone: "warning" },
} as const satisfies Record<OperatorInvoiceAttempt["outcome"], { key: string; tone: PressToast["tone"] }>;

const UNRECORDED: PressToast = { key: "invoicingCheckedUnrecorded", tone: "info" };

/**
 * What the toast after Check status or Resend says (#736, parent #731): what
 * the SRI answered the last call of this press, and nothing more - the rule
 * the resend hint follows (#728).
 *
 * NEVER THE STATUS. An answer that decides nothing (`received`, `unknown`)
 * leaves the status where it was, and so does a call that never reached the
 * SRI, so a toast read off the status restates the old one: on a document
 * refused with code 45, a Check the SRI answered `unknown` used to toast
 * "returned" - on the very press the Abandon flow asks for.
 *
 * THE PRESS NAMES ITS ATTEMPT. The API answers with `attempt_id`, the last
 * attempts row the press itself wrote, and the toast is that row's outcome.
 * The newest row is not read: a Sale Invoice Drainer round can write one
 * between the press and its answer. When no row is named or the named one is
 * missing - the ledger write failed - the toast does not guess an answer.
 */
export function pressToast(pressed: {
  attempt_id: number | null;
  attempts: readonly { id: number; outcome: OperatorInvoiceAttempt["outcome"] }[];
}): PressToast {
  const attempt = pressed.attempts.find((a) => a.id === pressed.attempt_id);
  // The map covers every outcome the type knows; `??` is for a word the API
  // learns before this page does, which is reported as unknown, not guessed.
  return attempt ? (PRESS_TOASTS[attempt.outcome] ?? UNRECORDED) : UNRECORDED;
}
