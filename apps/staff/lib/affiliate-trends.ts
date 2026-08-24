// Relative (not "@/lib") so the module graph resolves under `node --test` as
// well as the bundler — the unit tests import this file directly.
import { fetchEventsJSON } from "./events-api.ts";
import { staffIntlLocale, type AppLocale } from "./format.ts";

/**
 * Affiliate Link trends: how an Event's page traffic moved, hour by hour, split
 * by Affiliate Link and set against the page's whole traffic (ADR 0057, #411).
 *
 * The whole bounded history arrives in one request. Everything below the fetch
 * is a pure transform of it: range switching, legend toggling and re-bucketing
 * are decisions about data already in hand and never go back to the network.
 *
 * Every figure here is a floor, not a measurement (ADR 0022): buckets count
 * page loads, not people, and a Click is only ever the subset of loads that
 * carried a live link. Copy built over these numbers must say so.
 */

/** An Affiliate Link as the legend reads it: the Event's whole set, newest
 * first, deactivated ones included, so the legend never changes shape. */
export type AffiliateTrendsLink = {
  id: string;
  name: string;
  active: boolean;
};

/**
 * One stored hourly bucket, exactly as ADR 0057 keeps it: a UTC hour, whose
 * bucket it is, and a count — nothing else. A null `link_id` is the Event's
 * whole-page bucket, which every load lands in; a link's bucket counts the
 * subset that arrived through it while it was live.
 */
export type AffiliateViewBucket = {
  /** RFC3339, always a whole UTC hour. */
  hour: string;
  link_id: string | null;
  views: number;
};

/** One hour of derived Attributed Sales for one link. `hour` is a civil hour in
 * the EVENT's timezone ("2026-08-01T14:00"), unlike the view buckets' UTC. */
export type AffiliateSalesBucket = {
  hour: string;
  link_id: string;
  sales: number;
  tickets: number;
  net_proceeds_cents: number;
};

export type AffiliateTrends = {
  /** The Event's own zone (UTC where it carries none) — what every axis label
   * speaks, and what the sales hours are bucketed in. */
  timezone: string;
  /** The Organization's currency, which net_proceeds_cents is denominated in. */
  currency: string;
  links: AffiliateTrendsLink[];
  /** Sparse: an hour nothing happened in has no row. Zero-filled here. */
  view_buckets: AffiliateViewBucket[];
  /** Null — absent, not empty — on an Event that registers externally, whose
   * links are measured by clicks alone. Empty means measured, nothing sold. */
  sales_buckets: AffiliateSalesBucket[] | null;
};

/** fetchAffiliateTrends reads the Event's trends through the BFF. Org Admin /
 * Event Owner only — the Go API refuses anybody else. */
export async function fetchAffiliateTrends(eventId: string): Promise<AffiliateTrends> {
  return fetchEventsJSON<AffiliateTrends>(`/api/events/${eventId}/affiliate-links/trends`);
}

/**
 * The series id the Event's whole-page bucket is drawn under. A reserved word
 * beside the links' UUIDs — it cannot collide with one, and it lets the "All
 * page views" series ride the same selection, colour and datum machinery as any
 * link without being one.
 */
export const ALL_PAGE_VIEWS_ID = "all-page-views";

/**
 * The spans a reader can ask for. Not a granularity: each range carries its own
 * (`rangeGranularity`), because "how far back" and "how fine" are one decision —
 * a month of hourly bars would be four thousand slivers nobody can read, and a
 * day of daily bars would be one bar.
 */
export type AffiliateTrendsRange = "24h" | "7d" | "30d" | "all";

export const AFFILIATE_TRENDS_RANGES: readonly AffiliateTrendsRange[] = [
  "24h",
  "7d",
  "30d",
  "all",
] as const;

/** The first look: a week, hour by hour — recent enough to be actionable and
 * long enough to show a pattern. */
export const DEFAULT_AFFILIATE_TRENDS_RANGE: AffiliateTrendsRange = "7d";

export type AffiliateTrendsGranularity = "hour" | "day";

/**
 * rangeGranularity is the resolution a range is drawn at: hourly on the short
 * ranges, daily on the long ones, with no option to choose otherwise. Hourly is
 * also the permanent floor — the buckets hold nothing finer (ADR 0057).
 */
export function rangeGranularity(range: AffiliateTrendsRange): AffiliateTrendsGranularity {
  return range === "24h" || range === "7d" ? "hour" : "day";
}

/** How many whole hours each fixed range spans. */
const RANGE_HOURS: Record<Exclude<AffiliateTrendsRange, "all">, number> = {
  "24h": 24,
  "7d": 7 * 24,
  "30d": 30 * 24,
};

const HOUR_MS = 60 * 60 * 1000;

/** truncateToUTCHour floors an instant to its UTC hour — the key every stored
 * bucket is filed under. */
export function truncateToUTCHour(instant: Date): Date {
  return new Date(Math.floor(instant.getTime() / HOUR_MS) * HOUR_MS);
}

/** utcHourKey renders a whole UTC hour as the bucket key both the stored rows
 * and the drawn axis agree on: "2026-08-24T13:00:00Z". */
export function utcHourKey(instant: Date): string {
  return truncateToUTCHour(instant).toISOString().replace(".000Z", "Z");
}

/**
 * bucketDay is the CIVIL calendar day a UTC hour belongs to in the Event's
 * timezone — the key daily bars aggregate under.
 *
 * The Event's zone rather than UTC or the viewer's, and it matters at the
 * edges: an evening of Guayaquil page views (UTC-5) crosses midnight UTC while
 * the organizer's day carries on, and bucketing by UTC would split their
 * evening across two bars neither of which is a day they lived. This is the
 * same rule the Sales Trends days follow, so a click and the sale it drove land
 * on the same calendar day.
 *
 * "en-CA" is a formatting trick, not a locale choice: it is the one widely
 * shipped locale whose date pattern is exactly "YYYY-MM-DD", which makes the
 * formatter a timezone converter with a sortable key as its output. The reader
 * never sees this string — labels are made separately, in their language.
 */
export function bucketDay(utcHour: string, timezone: string): string {
  const parsed = new Date(utcHour);
  if (Number.isNaN(parsed.getTime())) {
    return utcHour;
  }
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(parsed);
}

/**
 * formatTrendsHour renders a whole UTC hour for the axis and the tooltip: the
 * day and the hour, in the EVENT's timezone and the reader's language —
 * "Aug 24, 14:00" / "24 ago, 14:00".
 *
 * Which hour a bar is belongs to the Event's clock; how it is spelled belongs
 * to the reader. Minutes are always ":00" — a bucket IS an hour — and stating
 * them anyway is what stops "14" reading as a count.
 */
export function formatTrendsHour(utcHour: string, timezone: string, locale: AppLocale): string {
  const parsed = new Date(utcHour);
  if (Number.isNaN(parsed.getTime())) {
    return utcHour;
  }
  return new Intl.DateTimeFormat(staffIntlLocale(locale), {
    timeZone: timezone,
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(parsed);
}

/**
 * formatTrendsDayLabel renders a "YYYY-MM-DD" civil day as "Aug 24" for the
 * daily ranges' axis. The key is ALREADY in the Event's timezone (`bucketDay`
 * put it there), so it is re-read in UTC on purpose: re-interpreting it in any
 * zone west of Greenwich would slide every bar back a day.
 */
export function formatTrendsDayLabel(day: string, locale: AppLocale): string {
  const parsed = new Date(`${day}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) {
    return day;
  }
  return new Intl.DateTimeFormat(staffIntlLocale(locale), {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(parsed);
}

/** One bucket as the chart draws it: key, label, and what each drawn series
 * counted there. No total — see MultiSeriesDatum for why there must not be one. */
export type AffiliateTrendsDatum = {
  key: string;
  label: string;
  values: Record<string, number>;
};

/**
 * viewsSeries turns the stored buckets into one datum per axis slot for the
 * chosen range: windowed, re-bucketed to the range's granularity, zero-filled,
 * and carrying only the selected series.
 *
 * The axis is contiguous from the start of the window to `now` — a quiet
 * afternoon reads as a row of zeros, not as two adjacent bars. On "all" it
 * starts at the earliest stored bucket (there is nothing to say before that:
 * buckets begin at launch, and the chart is not a ledger of the lifetime click
 * counter). An Event with no stored buckets yields no data at all rather than
 * an invented span.
 *
 * A deselected series is absent from `values`, not zeroed — filtering is a
 * filter. The whole-page bucket (null link_id) is drawn under
 * `ALL_PAGE_VIEWS_ID` and selected like anything else.
 */
export function viewsSeries(
  trends: Pick<AffiliateTrends, "timezone" | "view_buckets">,
  selected: readonly string[],
  range: AffiliateTrendsRange,
  now: Date,
  locale: AppLocale,
): AffiliateTrendsDatum[] {
  const granularity = rangeGranularity(range);
  const end = truncateToUTCHour(now);
  const start = windowStart(trends.view_buckets, range, end);
  if (start === null) {
    return [];
  }

  // Every axis slot, oldest first, each starting from zero for every selected
  // series — the zero-fill the sparse storage asks the client for.
  const data: AffiliateTrendsDatum[] = [];
  const slots = new Map<string, AffiliateTrendsDatum>();
  const pushSlot = (key: string, label: string) => {
    if (slots.has(key)) {
      return;
    }
    const values: Record<string, number> = {};
    for (const id of selected) {
      values[id] = 0;
    }
    const datum = { key, label, values };
    slots.set(key, datum);
    data.push(datum);
  };

  if (granularity === "hour") {
    for (let at = start.getTime(); at <= end.getTime(); at += HOUR_MS) {
      const hour = new Date(at);
      pushSlot(utcHourKey(hour), formatTrendsHour(hour.toISOString(), trends.timezone, locale));
    }
  } else {
    // Daily slots are civil days in the Event's zone, so the axis is built by
    // walking the window's hours through the same day-mapping the data takes —
    // the one way the two cannot disagree about which days exist.
    for (let at = start.getTime(); at <= end.getTime(); at += HOUR_MS) {
      const day = bucketDay(new Date(at).toISOString(), trends.timezone);
      pushSlot(day, formatTrendsDayLabel(day, locale));
    }
  }

  const drawn = new Set(selected);
  for (const bucket of trends.view_buckets) {
    const hour = new Date(bucket.hour);
    if (Number.isNaN(hour.getTime()) || hour.getTime() < start.getTime()) {
      continue;
    }
    const seriesId = bucket.link_id ?? ALL_PAGE_VIEWS_ID;
    if (!drawn.has(seriesId)) {
      continue;
    }
    const key =
      granularity === "hour" ? utcHourKey(hour) : bucketDay(bucket.hour, trends.timezone);
    const slot = slots.get(key);
    if (!slot) {
      // An hour after `now` — clock skew between the API and the reader. Better
      // an uncounted view than an axis slot in the future.
      continue;
    }
    // Accumulate rather than assign: hourly buckets are unique per series by
    // construction, but a daily slot legitimately sums many hours.
    slot.values[seriesId] += bucket.views;
  }

  return data;
}

/** The window's first hour, or null for "nothing to draw at all". */
function windowStart(
  buckets: readonly AffiliateViewBucket[],
  range: AffiliateTrendsRange,
  end: Date,
): Date | null {
  if (range !== "all") {
    // end - (n-1) hours, so the window is n slots INCLUDING the current hour:
    // "24h" is today's still-filling hour and the 23 before it.
    return new Date(end.getTime() - (RANGE_HOURS[range] - 1) * HOUR_MS);
  }
  let earliest: number | null = null;
  for (const bucket of buckets) {
    const at = new Date(bucket.hour).getTime();
    if (!Number.isNaN(at) && (earliest === null || at < earliest)) {
      earliest = at;
    }
  }
  if (earliest === null) {
    return null;
  }
  const start = truncateToUTCHour(new Date(earliest));
  return start.getTime() > end.getTime() ? end : start;
}

/**
 * toggleSeriesSelection adds or removes a series from the drawn set, returning
 * `order`'s order regardless of click order. The last drawn series cannot be
 * deselected, for the reason Sales Trends' chips refuse it: an empty chart says
 * nothing, and the reader deselecting their last chip already has the
 * single-series view they were reaching for.
 */
export function toggleSeriesSelection(
  order: readonly string[],
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
  return order.filter((entry) => next.has(entry));
}

/** The tick steps a Y axis is allowed to round up to, per decade — the same
 * ladder Sales Trends climbs, so the two surfaces' axes read alike. */
const NICE_STEPS = [1, 2, 2.5, 5, 10];

/**
 * multiSeriesYMax is the top of the Y axis for grouped bars: the tallest SINGLE
 * value, rounded up to a readable tick. The single value and never a sum,
 * because grouped bars stand beside each other — nothing in the chart ever
 * reaches the height a stack would.
 */
export function multiSeriesYMax(data: readonly AffiliateTrendsDatum[]): number {
  let tallest = 0;
  for (const datum of data) {
    for (const value of Object.values(datum.values)) {
      tallest = Math.max(tallest, value);
    }
  }
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
 * The horizontal room one bucket is owed: enough for one bar per drawn series
 * plus a gap, and never less than a Sales Trends day, so a single-link Event's
 * chart does not shrivel. More drawn series genuinely need more room — grouped
 * bars sit beside each other — which is why the series count is a parameter
 * rather than a constant.
 */
export const AFFILIATE_TRENDS_MIN_BAR_WIDTH = 10;
const MIN_BUCKET_WIDTH = 24;

/** The narrowest the plot is ever drawn, shared with Sales Trends' floor. */
export const AFFILIATE_TRENDS_MIN_PLOT_WIDTH = 360;

/**
 * affiliateTrendsPlotWidth is how wide the plot is drawn: the room the span
 * demands, or the room available, whichever is larger — the Sales Trends Daily
 * rule, and for the same reason. A week of hours cannot fit a card without
 * shaving every bucket to a sliver, so the plot outgrows the card and is read
 * by scrolling; a short span fills the card and the spare room goes to the gaps.
 */
export function affiliateTrendsPlotWidth(
  bucketCount: number,
  drawnSeriesCount: number,
  availableWidth = 0,
): number {
  const bucketWidth = Math.max(
    MIN_BUCKET_WIDTH,
    Math.max(1, drawnSeriesCount) * AFFILIATE_TRENDS_MIN_BAR_WIDTH,
  );
  const spanWidth = bucketCount <= 0 ? 0 : bucketCount * bucketWidth;
  return Math.max(spanWidth, availableWidth, AFFILIATE_TRENDS_MIN_PLOT_WIDTH);
}

/** hasPageViews reports whether anything has ever been counted — no buckets
 * (or all-zero ones) is the empty state, not an all-zero chart. */
export function hasPageViews(trends: Pick<AffiliateTrends, "view_buckets">): boolean {
  return trends.view_buckets.some((bucket) => bucket.views > 0);
}

/**
 * The measures the one chart can be switched between. `clicks` is always on
 * offer; `sales` only when the payload carries sales figures at all — an Event
 * that registers externally sends null, and its switcher must not exist rather
 * than sit disabled over a view with nothing behind it (#415). The list is
 * ordered as the switcher reads.
 */
export type AffiliateTrendsMetric = "clicks" | "sales";

export function availableTrendsMetrics(
  trends: Pick<AffiliateTrends, "sales_buckets">,
): AffiliateTrendsMetric[] {
  return trends.sales_buckets === null ? ["clicks"] : ["clicks", "sales"];
}

/**
 * hasTrendsData widens `hasPageViews` for the whole surface: Attributed Sales
 * derive from the sales ledger, which predates the buckets' launch, so an Event
 * can have a sales history worth drawing before a single page view is counted.
 */
export function hasTrendsData(
  trends: Pick<AffiliateTrends, "view_buckets" | "sales_buckets">,
): boolean {
  return (
    hasPageViews(trends) || (trends.sales_buckets?.some((bucket) => bucket.sales > 0) ?? false)
  );
}

/** What one link earned in one bucket, beyond the sale count the bar shows:
 * the tooltip's money line. Cents, in the Organization's currency. */
export type AffiliateSalesFigures = {
  tickets: number;
  netProceedsCents: number;
};

/** A sales-view datum: the drawn sale counts plus, per link, the figures the
 * tooltip states. Structurally a `MultiSeriesDatum` — the extra field rides
 * recharts' payload untouched. */
export type AffiliateSalesDatum = AffiliateTrendsDatum & {
  details: Record<string, AffiliateSalesFigures>;
};

/**
 * civilHourKey renders an instant as the Event-timezone civil hour the sales
 * buckets are keyed by ("2026-08-24T10:00"). The mapping only ever runs forward — the
 * civil keys the axis needs are computed FROM its UTC slots, never the reverse,
 * because a civil hour does not always name one instant (DST).
 */
function civilHourKey(instant: Date, timezone: string): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    hourCycle: "h23",
  }).formatToParts(instant);
  const part = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((entry) => entry.type === type)?.value ?? "";
  return `${part("year")}-${part("month")}-${part("day")}T${part("hour")}:00`;
}

/**
 * salesSeries is `viewsSeries` for the Attributed Sales view: one datum per
 * axis slot, windowed, zero-filled, carrying only the selected links — never
 * the whole-page series, which is a views concept with no sales to its name.
 *
 * Two asymmetries with the views:
 *
 * - The stored hours are CIVIL Event-timezone hours (that is how the sales are
 *   bucketed, ADR 0057), while the hourly axis is UTC slots. Each slot computes
 *   the civil hour it is, and claims the buckets keyed by it; where DST folds
 *   two slots onto one civil hour, the first slot claims it and the fold is
 *   counted once, not twice.
 *
 * - "all" reaches back to the EARLIEST of page views and sales, because the
 *   sales ledger predates the buckets' launch and truncating that history would
 *   misstate what a link earned.
 *
 * A Reversal is already gone from the payload, so it is gone from here.
 */
export function salesSeries(
  trends: Pick<AffiliateTrends, "timezone" | "view_buckets" | "sales_buckets">,
  selected: readonly string[],
  range: AffiliateTrendsRange,
  now: Date,
  locale: AppLocale,
): AffiliateSalesDatum[] {
  const salesBuckets = trends.sales_buckets;
  if (salesBuckets === null) {
    return [];
  }
  const granularity = rangeGranularity(range);
  const end = truncateToUTCHour(now);
  const drawn = selected.filter((id) => id !== ALL_PAGE_VIEWS_ID);

  // The window. Fixed ranges are the views' formula; "all" is the earliest
  // civil day either store knows, padded a rotation west so the walk below
  // reaches that day's first hour in any timezone.
  let earliestDay: string | null = null;
  let start: Date;
  if (range !== "all") {
    start = new Date(end.getTime() - (RANGE_HOURS[range] - 1) * HOUR_MS);
  } else {
    for (const bucket of trends.view_buckets) {
      const day = bucketDay(bucket.hour, trends.timezone);
      if (!Number.isNaN(new Date(bucket.hour).getTime()) && (earliestDay === null || day < earliestDay)) {
        earliestDay = day;
      }
    }
    for (const bucket of salesBuckets) {
      const day = bucket.hour.slice(0, 10);
      if (earliestDay === null || day < earliestDay) {
        earliestDay = day;
      }
    }
    if (earliestDay === null) {
      return [];
    }
    const dayStart = new Date(`${earliestDay}T00:00:00Z`);
    if (Number.isNaN(dayStart.getTime())) {
      return [];
    }
    start = new Date(Math.min(dayStart.getTime() - 14 * HOUR_MS, end.getTime()));
  }

  const data: AffiliateSalesDatum[] = [];
  const slots = new Map<string, AffiliateSalesDatum>();
  const pushSlot = (key: string, label: string) => {
    if (slots.has(key)) {
      return;
    }
    const values: Record<string, number> = {};
    const details: Record<string, AffiliateSalesFigures> = {};
    for (const id of drawn) {
      values[id] = 0;
      details[id] = { tickets: 0, netProceedsCents: 0 };
    }
    const datum = { key, label, values, details };
    slots.set(key, datum);
    data.push(datum);
  };

  // Which axis slot each civil hour is drawn in — first slot wins a DST fold.
  const slotOfCivilHour = new Map<string, string>();
  for (let at = start.getTime(); at <= end.getTime(); at += HOUR_MS) {
    const hour = new Date(at);
    if (granularity === "hour") {
      const key = utcHourKey(hour);
      pushSlot(key, formatTrendsHour(hour.toISOString(), trends.timezone, locale));
      const civil = civilHourKey(hour, trends.timezone);
      if (!slotOfCivilHour.has(civil)) {
        slotOfCivilHour.set(civil, key);
      }
    } else {
      const day = bucketDay(hour.toISOString(), trends.timezone);
      if (earliestDay !== null && day < earliestDay) {
        continue;
      }
      pushSlot(day, formatTrendsDayLabel(day, locale));
    }
  }

  const drawnSet = new Set(drawn);
  for (const bucket of salesBuckets) {
    if (!drawnSet.has(bucket.link_id)) {
      continue;
    }
    const key =
      granularity === "hour" ? slotOfCivilHour.get(bucket.hour) : bucket.hour.slice(0, 10);
    const slot = key === undefined ? undefined : slots.get(key);
    if (!slot) {
      // Outside the window, or an hour the walk never reached — skipped for the
      // reason viewsSeries skips: better an undrawn sale than an invented slot.
      continue;
    }
    slot.values[bucket.link_id] += bucket.sales;
    const figures = slot.details[bucket.link_id];
    figures.tickets += bucket.tickets;
    figures.netProceedsCents += bucket.net_proceeds_cents;
  }

  return data;
}
