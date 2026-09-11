import { DEFAULT_LOCALE, formatEventDateShort, type IntlLocale } from "./format.ts";

// Presentation rules for a Sales Cutoff — the optional closing instant after
// which a Ticket Type is still listed, still described, still priced and no
// longer buyable (ADR 0070).
//
// Nothing here decides whether a Ticket Type is closed. The server owns the
// clock and sends its verdict as `closed`; this app draws it. Deriving the
// verdict from `sales_cutoff_at` in the browser is the one thing this module
// must never do — a visitor whose machine is set to yesterday would be offered a
// stepper the API refuses, and one set to tomorrow would be denied a sale that
// is still open. The instant travels only so the card can SAY when the door
// shut, which is the question "did I miss it by an hour or by a month?".
//
// Sibling to lib/promotion.ts, whose closing-time idiom this borrows: a Ticket
// Type can carry both a Promotion and a Sales Cutoff at once.

/**
 * When sales closed, drawn in the Event's timezone: "Sun Jul 12, 6:00 PM".
 *
 * The Sales Cutoff is set in the Event's timezone (ADR 0070), so that is the
 * clock it is read back on — the same rule the Promotion deadline follows, and
 * for the same reason: a buyer in Madrid and one in Quito must be told the same
 * instant the organizer typed.
 *
 * Null when there is nothing honest to print — no cutoff was ever set, or the
 * value on the wire is not a date. A closed Ticket Type always has one, so the
 * null arm is the card losing its closing LINE and never its badge: the state is
 * the server's verdict, and it stands whether or not we can format the instant.
 */
export function formatSalesCutoff(
  salesCutoffAt: string | null,
  timezone: string | null,
  locale: IntlLocale = DEFAULT_LOCALE,
): string | null {
  return formatEventDateShort(salesCutoffAt, timezone, locale);
}
