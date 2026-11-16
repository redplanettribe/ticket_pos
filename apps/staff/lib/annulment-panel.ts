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

import type {
  AnnulmentVerdict,
  InvoiceStatus,
  OperatorAnnulmentCheck,
  OperatorAnnulmentWindow,
  OperatorInvoiceAnnulability,
} from "./operator-api";

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

/**
 * The Annulment Check's one state line on the annulled face (#746), decided
 * from the API's `annulment.check` alone:
 *
 * - checking since the recording, with the next ask;
 * - ANULADO at the ask that read it;
 * - still reported as authorized since the ask that settled the mismatch;
 * - the SRI didn't confirm at the +60 min ask, asking every 6 h until the
 *   schedule's end, or `until` null once the asks have ended;
 * - unchecked: recorded before the check existed, never asked.
 */
export type AnnulmentCheckLine =
  | { kind: "checking"; since: string; next: string | null }
  | { kind: "confirmed"; at: string }
  | { kind: "mismatch"; since: string }
  | { kind: "unconfirmed"; at: string; until: string | null }
  | { kind: "unchecked" };

export function annulmentCheckLine(check: OperatorAnnulmentCheck, annulledAt: string): AnnulmentCheckLine {
  if (!check.scheduled) {
    return { kind: "unchecked" };
  }
  switch (check.verdict) {
    case "confirmed":
      return { kind: "confirmed", at: check.since ?? annulledAt };
    case "mismatch":
      return { kind: "mismatch", since: check.since ?? annulledAt };
    case "unconfirmed":
      return {
        kind: "unconfirmed",
        at: check.since ?? annulledAt,
        until: check.next_ask_at === null ? null : check.asks_until,
      };
    default:
      return { kind: "checking", since: annulledAt, next: check.next_ask_at };
  }
}

/**
 * Whether the panel offers a Check status row: whenever the check is not
 * confirmed. On an annulled factura it is one more ask of ConsultaComprobante,
 * feeding the same verdict and leaving the schedule alone.
 */
export function annulmentCheckOffersCheckStatus(check: Pick<OperatorAnnulmentCheck, "verdict">): boolean {
  return check.verdict !== "confirmed";
}

/** The check badge's tone, one per verdict. */
export const ANNULMENT_CHECK_BADGE: Record<AnnulmentVerdict, "default" | "secondary" | "destructive" | "outline"> = {
  checking: "secondary",
  confirmed: "default",
  mismatch: "destructive",
  unconfirmed: "outline",
};
