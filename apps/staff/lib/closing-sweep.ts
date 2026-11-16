/**
 * The Closing Sweep and the Unrecorded Annulment on the staff app (#750, spec
 * #742, ADR 0078), decided once and tested under `node --test`.
 *
 * Once a month the platform asks the SRI about every authorized production
 * factura of the month just closed, and flags an Unrecorded Annulment where
 * the SRI reports one annulled. The platform never records the annulment
 * itself: a flagged factura carries a warning until an operator records it
 * with Mark annulled or, where a live Credit Note forbids that, acknowledges
 * it. The API decides every fact - the flag, whether the acknowledgement is
 * admitted, the dashboard's month and counts; this decides only what to draw.
 */

import type { OperatorClosingSweepStatus, OperatorUnrecordedAnnulment } from "./operator-api";

/**
 * What a flagged factura's panel offers: the record with Mark annulled where
 * it is admitted, the acknowledgement where a live Credit Note refuses it,
 * and once acknowledged who and when, offering nothing more.
 */
export type UnrecordedAnnulmentOffer =
  | { kind: "record"; foundAt: string }
  | { kind: "acknowledge"; foundAt: string }
  | { kind: "acknowledged"; foundAt: string; by: string; at: string };

/** The panel's line per offer. */
export const UNRECORDED_ANNULMENT_KEYS = {
  record: "invoicingUnrecordedRecordHint",
  acknowledge: "invoicingUnrecordedAcknowledgeHint",
  acknowledged: "invoicingUnrecordedAcknowledged",
} as const satisfies Record<UnrecordedAnnulmentOffer["kind"], string>;

/** The warning's offer, or null on a factura the sweep has not flagged. */
export function unrecordedAnnulmentOffer(invoice: {
  unrecorded_annulment: OperatorUnrecordedAnnulment | null;
  annulment: { acknowledge: { admitted: boolean; refusal: { code: string } | null } | null } | null;
}): UnrecordedAnnulmentOffer | null {
  const flag = invoice.unrecorded_annulment;
  if (flag === null) {
    return null;
  }
  if (flag.acknowledged_at !== null) {
    return { kind: "acknowledged", foundAt: flag.found_at, by: flag.acknowledged_by ?? "", at: flag.acknowledged_at };
  }
  if (invoice.annulment?.acknowledge?.admitted) {
    return { kind: "acknowledge", foundAt: flag.found_at };
  }
  return { kind: "record", foundAt: flag.found_at };
}

/**
 * The Operator Dashboard's line: "N facturas from <month> couldn't be swept",
 * or null when none are unswept. Sweep again is offered until every unswept
 * factura is already due on the next tick.
 */
export function closingSweepLine(
  status: OperatorClosingSweepStatus,
): { month: string; count: number; sweepAgain: boolean } | null {
  if (status.unswept_count === 0) {
    return null;
  }
  return {
    month: status.month,
    count: status.unswept_count,
    sweepAgain: status.due_again_count < status.unswept_count,
  };
}

/**
 * The API's `YYYY-MM` as the month's name and year in the reader's language,
 * read in UTC so no zone moves it into the month before. Anything else is
 * shown as sent.
 */
export function closingSweepMonthLabel(month: string, locale: string): string {
  if (!/^\d{4}-\d{2}$/.test(month)) {
    return month;
  }
  return new Intl.DateTimeFormat(locale, { month: "long", year: "numeric", timeZone: "UTC" }).format(
    new Date(`${month}-01T00:00:00Z`),
  );
}
