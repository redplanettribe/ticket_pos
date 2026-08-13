// Relative (not "@/lib") so the module graph resolves under `node --test` as
// well as the bundler — the unit tests import this file directly.
import { fetchEventsJSON, formatPriceCents } from "./events-api.ts";

/**
 * Sales Trends: how an Event's sales moved, day by day, split by Ticket Type
 * (CONTEXT.md, #274).
 *
 * The whole day × Ticket Type matrix arrives in one request. Everything below
 * the fetch is a pure transform of that matrix, because chip toggling must be
 * instant and must never go back to the network: filtering and rescaling are
 * decisions about data already in hand.
 */

/** A Ticket Type as the legend and the stacking order read it. */
export type TrendsTicketType = {
  id: string;
  name: string;
  /** The catalog's display order. Also what a colour is keyed on, so a Ticket
   * Type looks the same every time the tab is opened. */
  sort_order: number;
};

/** What one Ticket Type did on one day. Types that sold nothing are omitted. */
export type TrendsLine = {
  ticket_type_id: string;
  quantity: number;
  /** Takings (ADR 0040): what the sales earned the Organization on whatever
   * Sales Channel they sold — not Net Proceeds, which is a smaller figure
   * wherever the Event sold anywhere but online. */
  takings_cents: number;
};

/** One calendar day in the Event's timezone. `lines` is empty on a silent day. */
export type TrendsDay = {
  /** A plain calendar date, "2026-08-01" — already in the Event's timezone. */
  date: string;
  lines: TrendsLine[];
};

export type SalesTrends = {
  timezone: string;
  currency: string;
  /** The Event's whole catalog in display order, including Ticket Types that
   * have sold nothing, so the chip set does not change shape as sales arrive. */
  ticket_types: TrendsTicketType[];
  /** Contiguous and zero-filled: every day of the Event's selling life, quiet
   * ones included. Empty when the Event has sold nothing at all. */
  days: TrendsDay[];
  /** The Event's whole reversed count, independent of everything else. */
  reversed_count: number;
};

/**
 * Which figure a chart plots. Both callers are on the same surface: the tickets
 * chart and the Takings chart beneath it read the same matrix and differ only in
 * which field of a line they take.
 */
export type TrendsMeasure = "quantity" | "takings_cents";

/** One bar: a day, its axis label, what each drawn Ticket Type contributed, and
 * the height of the whole stack. */
export type TrendsDatum = {
  key: string;
  label: string;
  values: Record<string, number>;
  total: number;
};

/** fetchSalesTrends reads the Event's Sales Trends through the BFF. Org
 * Admin / Event Owner only — the Go API refuses anybody else. */
export async function fetchSalesTrends(eventId: string): Promise<SalesTrends> {
  return fetchEventsJSON<SalesTrends>(`/api/events/${eventId}/sales/trends`);
}

/**
 * toggleTicketTypeSelection adds or removes a Ticket Type from the drawn set,
 * returning catalog order regardless of the order chips were clicked in.
 *
 * Removing the last selected Ticket Type is refused: an empty chart says
 * nothing, and the reader almost certainly meant "show me only this one", which
 * is the state they are already in. They get there by deselecting the others,
 * and the single-Ticket-Type view is the floor rather than a step on the way to
 * a blank one.
 */
export function toggleTicketTypeSelection(
  catalog: readonly TrendsTicketType[],
  selected: readonly string[],
  id: string,
): string[] {
  const isSelected = selected.includes(id);
  if (isSelected && selected.length <= 1) {
    return [...selected];
  }
  const next = new Set(selected);
  if (isSelected) {
    next.delete(id);
  } else {
    next.add(id);
  }
  // Rebuild from the catalog so the stacking order is always the display order.
  return catalog.filter((type) => next.has(type.id)).map((type) => type.id);
}

/**
 * drawnTicketTypes narrows the catalog to the current selection, in catalog
 * display order — the order the bars stack in and the tooltip reads in.
 */
export function drawnTicketTypes(
  catalog: readonly TrendsTicketType[],
  selected: readonly string[],
): TrendsTicketType[] {
  return catalog.filter((type) => selected.includes(type.id));
}

/**
 * colorSlotFor is the palette slot a Ticket Type draws in: its position in the
 * whole catalog, which arrives in display order.
 *
 * Position rather than the Ticket Type's own sort_order, though sort_order is
 * what puts the catalog in that order. sort_order defaults to 0 in the schema,
 * so an Organization that never reordered its Ticket Types has every one of
 * them at 0 — keying colour on it directly would draw the entire stack in a
 * single colour and make the chart unreadable for exactly the Events nobody has
 * fussed over. Position carries the same order and cannot tie.
 *
 * Keyed on the whole catalog, never on the drawn subset, so a Ticket Type keeps
 * its colour whichever chips are switched off.
 */
export function colorSlotFor(catalog: readonly TrendsTicketType[], id: string): number {
  return catalog.findIndex((type) => type.id === id);
}

/**
 * trendsSeries turns the day × Ticket Type matrix into one datum per day for
 * the chosen measure, carrying only the selected Ticket Types.
 *
 * Deselected types are absent from `values` and absent from `total` — this is a
 * filter, not a visibility trick, so a hidden type cannot go on inflating the
 * bar it was removed from. A day with no sales keeps its slot with zeros, so a
 * quiet fortnight reads as a quiet fortnight rather than as two adjacent bars.
 */
export function trendsSeries(
  days: readonly TrendsDay[],
  selected: readonly string[],
  measure: TrendsMeasure,
): TrendsDatum[] {
  return days.map((day) => {
    const values: Record<string, number> = {};
    for (const id of selected) {
      values[id] = 0;
    }
    let total = 0;
    for (const line of day.lines) {
      if (!(line.ticket_type_id in values)) {
        continue;
      }
      const value = line[measure];
      // Accumulate rather than assign. The endpoint groups by (day, Ticket Type),
      // so today a type appears at most once in a day — but a second line would
      // then silently replace the first rather than sum with it, and the chart
      // would understate the day with nothing failing.
      values[line.ticket_type_id] += value;
      total += value;
    }
    return { key: day.date, label: formatTrendsDay(day.date), values, total };
  });
}

/**
 * The horizontal room one day is owed, in pixels.
 *
 * This is the number that makes a long selling period scroll instead of
 * compress. An Event on sale for a year has some hundreds of days in it, and
 * they cannot all be legible bars inside a card; the two ways out are to give a
 * bar less room or to make a bar mean more than a day. The second was rejected
 * during design (#278): a bar standing for a day at one range and a week at
 * another means a reader who learned the chart on a young Event misreads it on
 * an old one, and misreads it without noticing. So a day keeps its width, the
 * plot grows past the card, and the reader scrolls.
 */
export const TRENDS_MIN_BAR_WIDTH = 24;

/**
 * The narrowest the plot area is ever drawn, whatever the span.
 *
 * A three-day-old Event is three days wide — around seventy pixels — which reads
 * as a rendering fault rather than as a young Event. The floor gives those days
 * somewhere to sit. It does not make their bars any fatter: the chart caps a bar
 * at a maximum width, so a short span is a few normal bars spread out rather
 * than a few slabs, which is what keeps a bar looking like the same object at
 * every range.
 */
export const TRENDS_MIN_PLOT_WIDTH = 360;

/**
 * trendsPlotWidth is how wide the plotting area must be to give every day of the
 * span its own room — the width both charts are drawn at, so that a day sits at
 * the same horizontal position in each.
 *
 * It answers to the day count alone and never to the width available. That is
 * the point: a plot sized to its container is a plot that compresses, and one
 * sized to its contents is one that scrolls. It also means a span that fits
 * takes only the room it needs and leaves the rest of the card empty, rather
 * than stretching a fortnight across it.
 */
export function trendsPlotWidth(dayCount: number): number {
  if (dayCount <= 0) {
    return TRENDS_MIN_PLOT_WIDTH;
  }
  return Math.max(dayCount * TRENDS_MIN_BAR_WIDTH, TRENDS_MIN_PLOT_WIDTH);
}

/** The tick steps a Y axis is allowed to round up to, per decade. Chosen so the
 * axis lands on numbers a person reads without counting: 1, 2, 5, 10, 20 … */
const NICE_STEPS = [1, 2, 2.5, 5, 10];

/**
 * trendsYMax is the top of the Y axis for a set of drawn bars: the tallest
 * stack, rounded up to a readable tick.
 *
 * It is derived from the filtered data rather than from the whole catalog, so
 * deselecting the Ticket Type that dominated the Event genuinely gives the
 * remaining bars the full height of the chart. An all-zero or empty set gets a
 * floor of 1 so the axis draws a scale instead of collapsing to a line.
 */
export function trendsYMax(data: readonly TrendsDatum[]): number {
  const tallest = data.reduce((max, datum) => Math.max(max, datum.total), 0);
  if (tallest <= 0) {
    return 1;
  }
  const decade = 10 ** Math.floor(Math.log10(tallest));
  for (const step of NICE_STEPS) {
    const candidate = step * decade;
    if (tallest <= candidate) {
      return candidate;
    }
  }
  return 10 * decade;
}

/**
 * formatTakings renders a Takings figure exactly, in the Organization's
 * currency — what the tooltip shows, where the reader came for a precise number.
 *
 * It is the staff app's one money formatter (`formatPriceCents`), the same one
 * the Sales tab's Net Proceeds strip uses, so the two surfaces can differ in
 * what they count but never in how they spell it.
 */
export function formatTakings(cents: number, currency: string): string {
  return formatPriceCents(cents, currency);
}

/**
 * formatTakingsTick renders a Takings figure short enough for an axis tick:
 * "$1.3K" where the tooltip would say "$1,250.00".
 *
 * Abbreviating is not a style choice. The Y axis is a fixed width so the tickets
 * chart and the Takings chart line up, and an unabbreviated "$1,250,000.00"
 * would run out of it. The exact figure is a hover away, which is the right
 * place for it — an axis is read at a glance and a tooltip is read on purpose.
 */
export function formatTakingsTick(cents: number, currency: string): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency,
    notation: "compact",
    // Both bounds are stated: a currency's own default minimum would put ".0"
    // on every round tick ("$500.0"), which is noise on an axis.
    minimumFractionDigits: 0,
    maximumFractionDigits: 1,
  }).format(cents / 100);
}

/**
 * formatTrendsDay renders "2026-08-01" as "Aug 1" for the X axis and tooltip.
 *
 * The date arrives already resolved into the Event's timezone by the API, so it
 * is formatted in UTC on purpose: re-reading it in the viewer's zone is what
 * would slide a bar onto the wrong day, which is exactly the bug the Event
 * timezone bucketing exists to prevent.
 */
export function formatTrendsDay(date: string): string {
  const parsed = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) {
    return date;
  }
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(parsed);
}

/**
 * hasSales reports whether the Event has anything to chart at all — no days, or
 * days that are all silent. An Event with External Registration sells no Ticket
 * Sales and lands here too, which is why the empty state is worded about sales
 * rather than about days.
 */
export function hasSales(trends: SalesTrends): boolean {
  return trends.days.some((day) => day.lines.length > 0);
}
