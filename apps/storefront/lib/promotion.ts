import type { PublicPromotion } from "./api.ts";
import { DEFAULT_LOCALE, formatEventDateShort, type IntlLocale } from "./format.ts";

// Presentation rules for a Promotion — the time-boxed Promotional Price that
// overrides a Ticket Type's List Price for a scheduled window (ADR 0021).
//
// Nothing here decides whether a Promotion applies. The API sends `promotion`
// only while the window is live, and it has already priced `price_cents` at the
// Promotional Price when it does, both amounts carrying the service fee where
// the Event passes it on (ADR 0014). This app strikes through the List Price the
// server quoted and says when the window closes; it never re-prices anything.

/**
 * The "X% off" a Promotion is worth, or null when there is no honest number to
 * show.
 *
 * Rounded down, never up: a badge is a claim about money, and a buyer who is
 * promised 25% off must not find 24.6% at checkout. Under `pass_on` both amounts
 * carry the fee, so the percentage is off what the buyer actually pays, which is
 * the only figure they can check. A Promotion too small to reach a whole percent
 * gets no badge — the struck-through List Price beside it already tells the
 * story, and "0% off" would read as an insult.
 */
export function promotionSavingsPercent(promotion: PublicPromotion): number | null {
  const { list_price_cents: list, promotional_price_cents: promotional } = promotion;
  if (!Number.isFinite(list) || !Number.isFinite(promotional)) return null;
  if (list <= 0 || promotional < 0 || promotional >= list) return null;

  const percent = Math.floor(((list - promotional) / list) * 100);
  return percent >= 1 ? percent : null;
}

/**
 * When the Promotion stops, drawn in the Event's timezone: "Sat, Jul 12 ·
 * 7:00 PM".
 *
 * The window is scheduled in the Event's timezone (ADR 0021), so that is the
 * clock the deadline is read on — unlike the Reversal Window's deadline, which
 * is a platform-wide rule stated in Ecuador time (ADR 0018). Confusing the two
 * would print a time nobody involved agreed to.
 */
export function formatPromotionDeadline(
  promotion: PublicPromotion,
  timezone: string | null,
  locale: IntlLocale = DEFAULT_LOCALE,
): string | null {
  return formatEventDateShort(promotion.ends_at, timezone, locale);
}
