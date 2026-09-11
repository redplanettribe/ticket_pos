import {
  DEFAULT_LOCALE,
  formatEventDateShort,
  localDateKey,
  type IntlLocale,
} from "./format.ts";

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

/**
 * How many calendar days before a Sales Cutoff the Storefront starts counting
 * down.
 *
 * A presentation rule and not a term of sale, so it is a constant in one place
 * rather than a column an organizer sets: what "soon" means is the Storefront's
 * judgement about when a deadline becomes news, and it must read the same on
 * every Event. Private, and stated once, following the staff app's Low Stock
 * share (lib/ticket-type-availability.ts).
 *
 * Seven is a week: far enough out that a Customer can still act on it — a
 * weekend to decide, a payday to wait for — and near enough that it is not
 * always on. A badge that never goes dark stops being read.
 */
const COUNTDOWN_DAYS = 7;

/**
 * Whole calendar days from one "YYYY-MM-DD" key to another, negative when the
 * second falls first.
 *
 * Days and not hours. Both keys are read at midnight UTC — a zone neither key
 * was ever in — because the offsets cancel: the question is how many DATES apart
 * two dates are, and a DST shift inside either of their real days cannot change
 * that count. Subtracting the instants instead would put a 23-hour day one
 * short, which is the bug that makes a countdown skip a rung once a year.
 *
 * Private, and takes keys rather than instants, so the zone has already been
 * decided by the caller and cannot be forgotten here.
 */
function calendarDaysBetween(fromKey: string, toKey: string): number {
  const from = Date.parse(`${fromKey}T00:00:00Z`);
  const to = Date.parse(`${toKey}T00:00:00Z`);
  return Math.round((to - from) / 86_400_000);
}

/** What deciding a Ticket Type's one badge needs to know about it. */
export type BadgeableTicketType = {
  /** Capacity exhausted — the server's verdict, as `closed` is. */
  sold_out: boolean;
  /**
   * Past its Sales Cutoff, as judged by the server's clock (ADR 0070). Optional
   * and read as false when absent, so a caller that predates the cutoff has
   * nothing closed.
   */
  closed?: boolean;
  /** The Sales Cutoff as it was set, or null on a Ticket Type that never stops. */
  sales_cutoff_at: string | null;
};

/**
 * The ONE thing a Ticket Type's card says about its own availability beside its
 * name, or null when it has nothing to say and renders exactly as it did before
 * this feature existed.
 *
 * One badge and not a row of them (ADR 0070). Three badges at once tell a
 * Customer three things and leave them knowing none of them, and "Sold out"
 * beside "Sales closed" beside "Your limit reached" is a card arguing with
 * itself. The Promotion badge is not in this union and is not ranked against it:
 * it is a claim about price rather than about availability, and it keeps its own
 * place on a card that is still buyable.
 *
 * A flat union rather than a state plus an optional countdown, because the
 * countdown is not a fourth state a card can also be in — it is what an open
 * card says when there is a deadline worth mentioning, and only then.
 */
export type TicketTypeBadge =
  | { kind: "sold_out" }
  | { kind: "closed" }
  | { kind: "limit_reached" }
  | { kind: "closes_today" }
  | { kind: "closes_tomorrow" }
  | { kind: "days_left"; days: number }
  | null;

/** What a card needs to know beyond the Ticket Type itself to pick its badge. */
export type TicketTypeBadgeContext = {
  /**
   * Whether THIS reader has spent their Purchase Limit on an otherwise sellable
   * Ticket Type (lib/checkout.ts `allowanceSpent`). False on every surface that
   * does not know who is asking.
   */
  limitReached?: boolean;
  /** The Event's timezone — the calendar the days are counted on. */
  timezone?: string | null;
  /**
   * The instant to count down FROM, or null on a surface that never counts down.
   *
   * The read-only card on an ended Event passes nothing: it keeps the badge and
   * the closing line, because the record of an Event must match what a Customer
   * saw while it was selling, but a deadline on an Event that is already over is
   * not news, it is noise.
   */
  now?: Date | null;
};

/**
 * ticketTypeBadge ranks a Ticket Type's states into the single badge its card
 * wears: sold out, then closed, then limit reached, and only on a card in none
 * of those three, the countdown to its Sales Cutoff (ADR 0070).
 *
 * Sold out beats closed because it is the stronger fact and changes what the
 * buyer does next — "they are gone" ends the conversation, where "we stopped
 * selling" invites an email asking you to reopen. Limit reached goes last
 * because it is a statement about one reader rather than about the Event, and a
 * Customer must never read their own spent allowance as the Event being full.
 * The staff app ranks the same states in the same order
 * (lib/ticket-type-availability.ts) and lib/selection-url.ts blames a dropped
 * basket line by it, so a disagreement between the three is a bug rather than a
 * matter of taste.
 *
 * The day count is computed INSIDE the open branch and nowhere else, which is
 * what makes the dangerous case unreachable rather than merely unlikely: a
 * browser whose clock is set to next week could otherwise draw "Closes today"
 * over a card the server called closed, or a countdown over a sold-out one. The
 * worst a skewed clock can do here is put the number a day out.
 */
export function ticketTypeBadge(
  ticketType: BadgeableTicketType,
  context: TicketTypeBadgeContext = {},
): TicketTypeBadge {
  if (ticketType.sold_out) return { kind: "sold_out" };
  if (ticketType.closed) return { kind: "closed" };
  if (context.limitReached) return { kind: "limit_reached" };
  return countdownBadge(ticketType.sales_cutoff_at, context.timezone ?? null, context.now ?? null);
}

/**
 * The ladder, for a Ticket Type already known to be open: "Closes today",
 * "Closes tomorrow", then a number of days left up to seven, and nothing at all
 * beyond that.
 *
 * Counted in whole calendar days on the EVENT's calendar rather than in 24-hour
 * blocks, which is what makes the badge say the same thing all day: a page does
 * not change under somebody while they decide, and a buyer in Madrid and one in
 * Quito are told the same thing about the same Event. Counting in blocks would
 * flip "2 days left" to "1 day left" at some arbitrary hour of the afternoon and
 * would say different things to the two of them at the same moment.
 *
 * No hours, no minutes, no ticking clock. A countdown to the second turns a
 * deadline into a slot machine, and the last hour of a sale is not where this
 * platform wants a Customer's attention.
 *
 * Silent whenever there is nothing honest to count: no cutoff, an unreadable
 * one, an Event with no calendar of its own, a surface that never counts down,
 * or a reader whose clock has already run past a cutoff the server says has not
 * passed. That last one is the skewed clock, and silence is the only answer to
 * it that cannot be wrong — the server owns the verdict, and inventing a rung
 * from a clock we do not trust would be a claim nobody checked.
 */
function countdownBadge(
  salesCutoffAt: string | null,
  timezone: string | null,
  now: Date | null,
): TicketTypeBadge {
  if (!salesCutoffAt || !timezone || !now) return null;
  const cutoff = new Date(salesCutoffAt);
  if (Number.isNaN(cutoff.getTime()) || Number.isNaN(now.getTime())) return null;

  const days = calendarDaysBetween(localDateKey(now, timezone), localDateKey(cutoff, timezone));
  if (!Number.isInteger(days) || days < 0 || days > COUNTDOWN_DAYS) return null;
  if (days === 0) return { kind: "closes_today" };
  if (days === 1) return { kind: "closes_tomorrow" };
  return { kind: "days_left", days };
}
