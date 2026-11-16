/**
 * The annulment panel on a factura's detail page (#745, spec #742, ADR 0078),
 * decided once and tested under `node --test`.
 *
 * The API is the gate and the clock: `annulability` says whether Mark annulled
 * is admitted, and if not why, and carries the Annulment Window with every date
 * already worked out in Guayaquil. What is left for the page is which face of
 * the panel to draw, where today falls on the window's timeline, which tone the
 * "Annullable until" line takes, and when the record form may be pressed. It
 * never computes a date.
 */

import type { InvoiceStatus, OperatorAnnulmentWindow, OperatorInvoiceAnnulability } from "./operator-api";

/**
 * The refusals a factura's panel shrinks to one sentence for (story 29): facts
 * about the document that keep Mark annulled from it while it otherwise could
 * be annulled. Any other refusal - owed, unsigned, withdrawn, abandoned - is a
 * state the act never reaches, and the panel is not drawn at all.
 */
export const ANNULMENT_PANEL_REFUSALS = [
  "INVOICE_ABANDON_INSTEAD",
  "INVOICE_CREDIT_NOTE_STANDS",
  "INVOICE_REISSUE_STANDS",
] as const;

export type AnnulmentPanelMode = "record" | "annulled" | "refused" | null;

/**
 * Which face the panel shows: the record form on a factura the act admits,
 * "Annulled at the SRI" once it was annulled, one sentence where a fact about
 * it refuses, and nothing elsewhere. A Credit Note has no annulability and no
 * panel: it keeps the SRI-remedies card's Mark annulled.
 */
export function annulmentPanelMode(invoice: {
  status: InvoiceStatus;
  annulability: OperatorInvoiceAnnulability | null;
}): AnnulmentPanelMode {
  const annulability = invoice.annulability;
  if (annulability === null) {
    return null;
  }
  if (invoice.status === "annulled") {
    return "annulled";
  }
  if (annulability.admitted) {
    return "record";
  }
  const code = annulability.refusal?.code;
  if (code !== undefined && (ANNULMENT_PANEL_REFUSALS as readonly string[]).includes(code)) {
    return "refused";
  }
  return null;
}

/**
 * Where today falls on the timeline from the Emission Date to the deadline, as
 * a fraction from 0 to 1: at the end on the last day and once closed.
 */
export function annulmentWindowProgress(window: OperatorAnnulmentWindow): number {
  if (!window.open || window.days_total <= 0) {
    return 1;
  }
  const elapsed = window.days_total - window.days_left;
  return Math.min(1, Math.max(0, elapsed / window.days_total));
}

/**
 * The "Annullable until" line's tone: quiet while open, a warning from day 1 of
 * the following month to the deadline (story 16), greyed once closed.
 */
export function annulmentWindowTone(window: OperatorAnnulmentWindow): "open" | "warning" | "closed" {
  if (!window.open) {
    return "closed";
  }
  return window.warning ? "warning" : "open";
}

/**
 * Whether the record form may be pressed: the number typed in full, and past
 * the window the acknowledgement typed as the phrase the form names, in the
 * reader's language and whatever its case. The API checks the number again
 * and refuses a mismatch; this only keeps a certain refusal from being sent.
 */
export function annulmentRecordReady(input: {
  number: string;
  typedNumber: string;
  late: boolean;
  typedAcknowledgement: string;
  phrase: string;
}): boolean {
  if (input.typedNumber.trim() !== input.number) {
    return false;
  }
  if (!input.late) {
    return true;
  }
  return input.typedAcknowledgement.trim().toLocaleLowerCase() === input.phrase.trim().toLocaleLowerCase();
}
