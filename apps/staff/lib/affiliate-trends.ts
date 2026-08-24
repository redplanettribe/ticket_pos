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
 * default (`rangeGranularity`), because "how far back" and "how fine" arrive
 * together — a week is read by the hour, a month by the day — though the
 * reader can overrule the default with the Hourly/Daily toggle
 * (`trendsGranularity`).
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

export const AFFILIATE_TRENDS_GRANULARITIES: readonly AffiliateTrendsGranularity[] = [
  "hour",
  "day",
] as const;

/**
 * rangeGranularity is the resolution a range opens at: hourly on the short
 * ranges, daily on the long ones. A default, not a rule — the reader switches
 * with the toggle, and a range switch resets to this so a month never opens as
 * seven hundred hourly slivers unasked. Hourly is the permanent floor — the
 * buckets hold nothing finer (ADR 0057).
 */
export function rangeGranularity(range: AffiliateTrendsRange): AffiliateTrendsGranularity {
  return range === "24h" || range === "7d" ? "hour" : "day";
}

/**
 * trendsGranularity is the resolution actually drawn for a range and the
 * reader's choice: the choice, except on 24h, which is always hourly — a day of
 * daily points is one point, and one point is not a trend. The toggle is not offered
 * there (`granularityChoosable`), and this guards the state it would have set.
 */
export function trendsGranularity(
  range: AffiliateTrendsRange,
  chosen: AffiliateTrendsGranularity,
): AffiliateTrendsGranularity {
  return range === "24h" ? "hour" : chosen;
}

/** granularityChoosable reports whether the Hourly/Daily toggle has anything
 * to offer on a range — false on 24h, where daily is one point. */
export function granularityChoosable(range: AffiliateTrendsRange): boolean {
  return range !== "24h";
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
 * chosen range: windowed, re-bucketed to the granularity asked for (the range's
 * own default when none is), zero-filled, and carrying only the selected
 * series.
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
  granularity: AffiliateTrendsGranularity = rangeGranularity(range),
): AffiliateTrendsDatum[] {
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

/**
 * The tick steps a Y axis is allowed to round up to, per decade.
 *
 * A finer ladder than Sales Trends' 1, 2, 2.5, 5, 10. A stack of bars is read
 * against its neighbours, so a coarse top costs it little; a line is read
 * against the axis, and a week whose busiest day drew 56 views was drawn under
 * a 100 axis with the whole upper half of the plot empty. This ladder tops
 * that day at 60, and never draws the tallest point below half the axis.
 *
 * Every rung must still divide into whole, even ticks by `trendsYTicks`'
 * divisions (5, 4, 2): 3 and 6 divide by 2 in the tens and above, and in the
 * ones decade a top of 3 is labelled at its ends alone, which is what a
 * three-view day deserves. 2.5 has no place here — it would put a tick at
 * 0.5 of a page view.
 */
const NICE_STEPS = [1, 2, 3, 4, 5, 6, 8, 10];

/**
 * multiSeriesYMax is the top of the Y axis for the counting views: the tallest
 * SINGLE value, rounded up to a readable tick. The single value and never a
 * sum, because each series is its own line — nothing in the chart ever reaches
 * the height a stack would.
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
 * The horizontal room one bucket is owed: a Sales Trends day's worth, whether
 * the bucket is an hour or a day. The counting views are drawn as lines, so a
 * bucket is one point per series wherever it lies, and drawing more series
 * asks for no more room — the old grouped-bar rule, which widened the bucket
 * per drawn series, would with a dozen links turn a week of hours into a
 * plot several screens wider than the reader could follow.
 */
export const AFFILIATE_TRENDS_BUCKET_WIDTH = 24;

/** The narrowest the plot is ever drawn, shared with Sales Trends' floor. */
export const AFFILIATE_TRENDS_MIN_PLOT_WIDTH = 360;

/**
 * affiliateTrendsPlotWidth is how wide the plot is drawn: the room the span
 * demands, or the room available, whichever is larger — the Sales Trends Daily
 * rule, and for the same reason. A week of hours cannot fit a card without
 * shaving every bucket to a sliver, so the plot outgrows the card and is read
 * by scrolling; a short span fills the card and the spare room spreads the
 * points out. The series count has no say: see `AFFILIATE_TRENDS_BUCKET_WIDTH`.
 */
export function affiliateTrendsPlotWidth(bucketCount: number, availableWidth = 0): number {
  const spanWidth = bucketCount <= 0 ? 0 : bucketCount * AFFILIATE_TRENDS_BUCKET_WIDTH;
  return Math.max(spanWidth, availableWidth, AFFILIATE_TRENDS_MIN_PLOT_WIDTH);
}

/**
 * latestBucketWithData is the index of the newest axis slot any drawn series
 * has something in, or -1 when the whole window is empty. The scrolling window
 * opens on this bucket rather than on "now": the newest hours of a live range
 * are routinely still empty, and a viewport anchored to them shows a reader a
 * row of zeros beside a chart they cannot tell has data further left.
 */
export function latestBucketWithData(
  data: readonly { values: Record<string, number | null> }[],
): number {
  for (let index = data.length - 1; index >= 0; index -= 1) {
    for (const value of Object.values(data[index].values)) {
      if (value !== null && value > 0) {
        return index;
      }
    }
  }
  return -1;
}

/**
 * latestBucketsScrollLeft is the scroll offset that puts the newest bucket with
 * data at the viewport's right edge — or the plot's end when nothing in the
 * window has data, which is the old "now" anchor and the only honest place
 * left. Pure arithmetic over the chart-frame geometry the caller passes in
 * (the plot starts `plotLeft` in from the content's left edge and is followed
 * by `plotRight` of margin), so it can be tested without a DOM.
 */
export function latestBucketsScrollLeft(
  data: readonly { values: Record<string, number | null> }[],
  geometry: {
    plotWidth: number;
    plotLeft: number;
    plotRight: number;
    clientWidth: number;
    scrollWidth: number;
  },
): number {
  const overflow = Math.max(0, geometry.scrollWidth - geometry.clientWidth);
  const index = latestBucketWithData(data);
  if (index < 0) {
    return overflow;
  }
  const bucketRight = geometry.plotLeft + ((index + 1) / data.length) * geometry.plotWidth;
  const target = bucketRight + geometry.plotRight - geometry.clientWidth;
  return Math.min(overflow, Math.max(0, Math.round(target)));
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
export type AffiliateTrendsMetric = "clicks" | "sales" | "rate";

export function availableTrendsMetrics(
  trends: Pick<AffiliateTrends, "sales_buckets">,
): AffiliateTrendsMetric[] {
  return trends.sales_buckets === null ? ["clicks"] : ["clicks", "sales", "rate"];
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

/** What one link earned in one bucket, beyond the sale count the point shows:
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
  granularity: AffiliateTrendsGranularity = rangeGranularity(range),
): AffiliateSalesDatum[] {
  const salesBuckets = trends.sales_buckets;
  if (salesBuckets === null) {
    return [];
  }
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

/**
 * The Attribution Rate view's own counting: `cumulative` divides everything to
 * date, `daily` divides each day by itself — Sales Trends' pair of views, worn
 * by a division instead of a sum.
 *
 * Cumulative is the default because it is the honest first look: last-click
 * attribution follows a link for up to seven days (ADR 0022), so a day's sales
 * routinely answer an earlier day's clicks, and the daily quotient swings hard
 * on small numbers. The running division absorbs the lag; the Daily view is
 * offered for the reader who wants the swings, knowing what they are.
 */
export type AffiliateRateView = "cumulative" | "daily";

export const DEFAULT_AFFILIATE_RATE_VIEW: AffiliateRateView = "cumulative";

/**
 * The ranges the Rate view offers: the shared ladder minus 24h. A rate is never
 * drawn hourly (ADR 0057) — with a seven-day window between click and sale, an
 * hourly division manufactures rates over 100% and divisions by zero — and a
 * "24 hours" of daily points is one point pretending to be a range. The reader
 * arriving on 24h is shown the week instead.
 */
export const RATE_TRENDS_RANGES: readonly AffiliateTrendsRange[] = ["7d", "30d", "all"];

/** rateRange is the range the Rate view actually draws for a chosen one:
 * itself, except 24h, which honesty widens to the week. */
export function rateRange(range: AffiliateTrendsRange): AffiliateTrendsRange {
  return range === "24h" ? "7d" : range;
}

/** The two counts a rate point divides — kept beside the quotient so the
 * tooltip states the division rather than a bare percentage. */
export type AffiliateRateFigures = {
  sales: number;
  denominator: number;
};

/**
 * A Rate-view datum. `values` admits null where the other views' cannot: a
 * span with no clicks has no rate — not a zero (which would say "clicks came
 * and nobody bought") and not an infinity — and the chart draws a gap there.
 */
export type AffiliateRateDatum = {
  key: string;
  label: string;
  values: Record<string, number | null>;
  details: Record<string, AffiliateRateFigures>;
};

/**
 * rateSeries draws the Attribution Rate: per link, Attributed Sales over
 * Clicks; and under `ALL_PAGE_VIEWS_ID`, every attributed sale over every page
 * view — the page's own rate, which is what the whole-page chip means on this
 * view.
 *
 * Always daily points, whatever the range's own granularity (`rateRange` has
 * already widened 24h away). Cumulative divides everything to date INCLUDING
 * history before the window — the window chooses which days are shown, never
 * which sales count — so a quiet week is a flat line, not a cliff. Daily
 * divides each day by itself. Either way a zero-denominator span yields null:
 * no clicks, no rate, no point.
 *
 * "To date" begins on the first day a page view was counted. The sales ledger
 * predates the buckets' launch (ADR 0057) and the counter does not, so a sale
 * from before that day has its clicks nowhere in the data — counting it would
 * divide a lifetime of sales by a launch day's handful of views and draw an
 * 84,000% rate. Numerator and denominator cover the same span, or the quotient
 * means nothing. The same floor bounds "all": before the first counted view
 * there is no rate to draw on either view. One floor for every series rather
 * than one per link: a link created after launch has buckets from its first
 * day, so the global floor already covers it.
 *
 * A Reversal is already out of the payload, so every past point it touched has
 * already moved.
 */
export function rateSeries(
  trends: Pick<AffiliateTrends, "timezone" | "view_buckets" | "sales_buckets">,
  selected: readonly string[],
  range: AffiliateTrendsRange,
  now: Date,
  locale: AppLocale,
  view: AffiliateRateView,
): AffiliateRateDatum[] {
  const salesBuckets = trends.sales_buckets;
  if (salesBuckets === null) {
    return [];
  }
  const effective = rateRange(range);
  const end = truncateToUTCHour(now);
  const drawn = [...selected];

  // Whole-history day tallies, whatever the window: the cumulative view needs
  // every day there ever was, and the daily view simply reads fewer of them.
  // A link's clicks and sales tally under its id; the page's views and ALL
  // attributed sales tally under ALL_PAGE_VIEWS_ID — numerator and denominator
  // of the overall line.
  // A bucket on a civil day after the reader's own — clock skew between the
  // API and the reader across a midnight — is no more "to date" here than it
  // is drawable on the other views; it waits for its day to exist.
  const lastDay = bucketDay(utcHourKey(end), trends.timezone);
  const clicksByDay = new Map<string, Map<string, number>>();
  const salesByDay = new Map<string, Map<string, number>>();
  const tally = (store: Map<string, Map<string, number>>, day: string, id: string, n: number) => {
    const forDay = store.get(day) ?? new Map<string, number>();
    forDay.set(id, (forDay.get(id) ?? 0) + n);
    store.set(day, forDay);
  };
  for (const bucket of trends.view_buckets) {
    if (Number.isNaN(new Date(bucket.hour).getTime())) {
      continue;
    }
    const day = bucketDay(bucket.hour, trends.timezone);
    if (day > lastDay) {
      continue;
    }
    tally(clicksByDay, day, bucket.link_id ?? ALL_PAGE_VIEWS_ID, bucket.views);
  }
  // The first day anything was counted: where "to date" starts, and the
  // earliest day either view can draw a rate on.
  let firstViewDay: string | null = null;
  for (const day of clicksByDay.keys()) {
    if (firstViewDay === null || day < firstViewDay) {
      firstViewDay = day;
    }
  }
  for (const bucket of salesBuckets) {
    const day = bucket.hour.slice(0, 10);
    if (day > lastDay || firstViewDay === null || day < firstViewDay) {
      continue;
    }
    tally(salesByDay, day, bucket.link_id, bucket.sales);
    tally(salesByDay, day, ALL_PAGE_VIEWS_ID, bucket.sales);
  }

  // The window's day slots, walked the same way the sales view walks its own:
  // hour by hour through the same day-mapping the data took.
  let earliestDay: string | null = null;
  let start: Date;
  if (effective !== "all") {
    start = new Date(end.getTime() - (RANGE_HOURS[effective] - 1) * HOUR_MS);
  } else {
    earliestDay = firstViewDay;
    if (earliestDay === null) {
      return [];
    }
    const dayStart = new Date(`${earliestDay}T00:00:00Z`);
    if (Number.isNaN(dayStart.getTime())) {
      return [];
    }
    start = new Date(Math.min(dayStart.getTime() - 14 * HOUR_MS, end.getTime()));
  }

  const slotDays: string[] = [];
  const seen = new Set<string>();
  for (let at = start.getTime(); at <= end.getTime(); at += HOUR_MS) {
    const day = bucketDay(new Date(at).toISOString(), trends.timezone);
    if (seen.has(day) || (earliestDay !== null && day < earliestDay)) {
      continue;
    }
    seen.add(day);
    slotDays.push(day);
  }

  // Cumulative running sums start from the beginning of history, so by the
  // time the first shown day is reached they already carry everything before
  // the window. The days are walked in order; slot days emit a datum.
  const running = { clicks: new Map<string, number>(), sales: new Map<string, number>() };
  if (view === "cumulative") {
    const allDays = [...new Set([...clicksByDay.keys(), ...salesByDay.keys()])]
      .filter((day) => slotDays.length > 0 && day < slotDays[0])
      .sort();
    for (const day of allDays) {
      accumulateDay(running, clicksByDay, salesByDay, day);
    }
  }

  return slotDays.map((day) => {
    if (view === "cumulative") {
      accumulateDay(running, clicksByDay, salesByDay, day);
    }
    const values: Record<string, number | null> = {};
    const details: Record<string, AffiliateRateFigures> = {};
    for (const id of drawn) {
      const denominator =
        view === "cumulative"
          ? (running.clicks.get(id) ?? 0)
          : (clicksByDay.get(day)?.get(id) ?? 0);
      const sales =
        view === "cumulative" ? (running.sales.get(id) ?? 0) : (salesByDay.get(day)?.get(id) ?? 0);
      values[id] = denominator > 0 ? sales / denominator : null;
      details[id] = { sales, denominator };
    }
    return { key: day, label: formatTrendsDayLabel(day, locale), values, details };
  });
}

function accumulateDay(
  running: { clicks: Map<string, number>; sales: Map<string, number> },
  clicksByDay: Map<string, Map<string, number>>,
  salesByDay: Map<string, Map<string, number>>,
  day: string,
): void {
  for (const [id, n] of clicksByDay.get(day) ?? []) {
    running.clicks.set(id, (running.clicks.get(id) ?? 0) + n);
  }
  for (const [id, n] of salesByDay.get(day) ?? []) {
    running.sales.set(id, (running.sales.get(id) ?? 0) + n);
  }
}

/**
 * rateYMax is the top of the Rate view's axis: the highest drawn point,
 * rounded up the same NICE ladder the counting views climb — the ladder works
 * below 1 because the decade arithmetic does, so a 5.2% peak tops the axis at
 * 6%, not 10%. An empty or all-gap view keeps a 5% axis, so the chart shows a
 * scale rather than collapsing.
 */
export function rateYMax(data: readonly AffiliateRateDatum[]): number {
  let tallest = 0;
  for (const datum of data) {
    for (const value of Object.values(datum.values)) {
      if (value !== null) {
        tallest = Math.max(tallest, value);
      }
    }
  }
  if (tallest <= 0) {
    return 0.05;
  }
  const decade = 10 ** Math.floor(Math.log10(tallest));
  for (const step of NICE_STEPS) {
    const candidate = wholeBasisPoints(step * decade);
    if (tallest <= candidate + Number.EPSILON) {
      return candidate;
    }
  }
  return wholeBasisPoints(10 * decade);
}

/**
 * A rung of the ladder settled to whole basis points, because 3 × 0.1 is
 * 0.30000000000000004 in floating point, and an axis topped there draws its
 * last tick a hair below the top. A rate too small to have a basis point at
 * all is left alone rather than rounded to nothing.
 */
function wholeBasisPoints(rate: number): number {
  const rounded = Math.round(rate * 10000) / 10000;
  return rounded > 0 ? rounded : rate;
}

/**
 * rateYTicks divides the rate axis the way `trendsYTicks` divides a counting
 * one, computed in whole basis points so the fractions come out exact — five
 * equal steps of floating-point 0.01 would land a tick at 0.030000000000000002
 * and label it 3%.
 *
 * A step must also be a whole tenth of a percent, because that is the finest
 * the axis spells (`formatPercent` keeps one decimal): five steps of 0.12%
 * would be labelled 0.1%, 0.2%, 0.4%, 0.5% — a scale that lies. A top too
 * small to divide that finely is labelled at its ends alone.
 */
export function rateYTicks(yMax: number): number[] {
  if (!Number.isFinite(yMax) || yMax <= 0) {
    return [0];
  }
  const scaled = Math.round(yMax * 10000);
  const divisions =
    Y_DIVISIONS.find((count) => scaled % (count * 10) === 0 && roundStep(scaled / count)) ??
    Y_DIVISIONS.find((count) => scaled % (count * 10) === 0) ??
    1;
  return Array.from({ length: divisions + 1 }, (_, index) => (scaled / divisions) * index / 10000);
}

/**
 * The divisions a Y axis is tried in, roundest step first. Sales Trends'
 * `trendsYTicks` settles for the first count that divides evenly, which on the
 * finer ladder these charts climb (`NICE_STEPS`) labels an axis topping at 80
 * in sixteens. A reader counts in tens and twenties, so a division is only
 * taken when its step starts with a 1, 2 or 5 — 80 in twenties, 60 in
 * twenties, 40 in tens, 30 in tens — and any even division is the fallback.
 */
const Y_DIVISIONS = [5, 4, 3, 2];

/** roundStep says whether a step is one a reader counts in: a 1, 2 or 5
 * followed by zeros. */
function roundStep(step: number): boolean {
  const leading = step / 10 ** Math.floor(Math.log10(step));
  return leading === 1 || leading === 2 || leading === 5;
}

/**
 * countYTicks is `trendsYTicks` for the counting views — the same evenly
 * spaced ticks ending at `yMax`, choosing the division by `Y_DIVISIONS`' rule
 * rather than the first that happens to be whole.
 */
export function countYTicks(yMax: number): number[] {
  if (!Number.isFinite(yMax) || yMax <= 0) {
    return [0];
  }
  const divisions =
    Y_DIVISIONS.find((count) => Number.isInteger(yMax / count) && roundStep(yMax / count)) ??
    Y_DIVISIONS.find((count) => Number.isInteger(yMax / count));
  if (!divisions) {
    return [0, yMax];
  }
  const step = yMax / divisions;
  return Array.from({ length: divisions + 1 }, (_, index) => index * step);
}
