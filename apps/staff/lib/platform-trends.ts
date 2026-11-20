// Platform Trends (#821, spec #808, ADR 0080): the growth reading at the top
// of the Operator Dashboard's Overview.
//
// The backend answers everything the section draws: the buckets, the totals,
// the change against the comparison span, which bucket is partial, and the day
// exact recording began. What is here is the little the page does with that
// answer before drawing it: the reading's address (span and comparison, in the
// URL as the Tax Invoices list keeps its filters), the request that address
// names, the spelling of a change and of a bucket, and the reshaping of the
// response into what the shared charts and the shared tile take.
//
// Nothing here buckets, totals a span, compares two spans or decides what is
// partial. The two places a number is added up are drawing a stack: the height
// of the Tickets Sold bar and of the line it is compared with, which is what a
// stacked chart is.
//
// Relative imports carry the ".ts" extension so the module graph resolves
// under `node --test` as well as the bundler.
import { fetchEventsJSON } from "./events-api.ts";
import { formatNumber, staffIntlLocale, type AppLocale } from "./format.ts";
import { axisTopFor } from "./sales-trends.ts";

// ---------------------------------------------------------------------------
// The response, field for field as GET /api/v1/operator/platform-trends
// states it.
// ---------------------------------------------------------------------------

/** One bucket of a series: a day, or the days of a week, both ends included. */
export type PlatformTrendBucket = {
  first_day: string;
  last_day: string;
  /** A count, or for money an amount in the minor units of the series' currency. */
  value: number;
  /** True on today's bucket alone: a day still running. */
  partial: boolean;
};

export type PlatformTrendSeries = {
  /** The Sales Channel, the currency code, or "all". */
  key: string;
  /** The series' total for the span, today excluded. */
  total: number;
  /**
   * One entry per bucket of the span, null where the bucket lies wholly
   * before the figure's data_since: nothing was counted there, which is not
   * a zero. A bucket holding data_since starts on it.
   */
  buckets: (PlatformTrendBucket | null)[];
  /** Today, in no total. Null when the span ends before yesterday. */
  today: PlatformTrendBucket | null;
  comparison_total: number | null;
  /** A signed ratio, or null when there is nothing to compare against. */
  change: number | null;
  /** One entry per entry of `buckets`, null where the comparison has no data. */
  comparison_buckets: (PlatformTrendBucket | null)[];
};

export type PlatformTrendFigure = {
  recorded: boolean;
  /** The day exact recording began, or null when the figure was always exact. */
  exact_since: string | null;
  data_since: string | null;
  /** Null for money, whose currencies are never added together. */
  total: number | null;
  comparison_total: number | null;
  change: number | null;
  series: PlatformTrendSeries[];
};

/** One bucket of an activity figure: its frozen days and their mean. */
export type PlatformTrendActivityBucket = {
  first_day: string;
  last_day: string;
  value: number;
};

export type PlatformTrendActivityFigure = {
  /** False while the platform does not record the figure at all. */
  recorded: boolean;
  exact_since: string | null;
  data_since: string | null;
  /** True while every frozen day of Active Organizations counts sales alone. */
  sale_half_only: boolean;
  /** The mean daily count over the span's frozen days. */
  mean: number | null;
  comparison_mean: number | null;
  change: number | null;
  distinct_7d: number | null;
  distinct_30d: number | null;
  /** How many of the 7 and the 30 days each distinct count is over are
   * recorded; null exactly when its count is. */
  distinct_7d_days: number | null;
  distinct_30d_days: number | null;
  /** One entry per bucket of the span; null where no day of it is frozen. */
  buckets: (PlatformTrendActivityBucket | null)[];
  comparison_buckets: (PlatformTrendActivityBucket | null)[];
};

export type PlatformTrendGranularity = "day" | "week";

export type PlatformTrendDays = { first_day: string; last_day: string };

export type PlatformTrends = {
  /** The platform clock every day here is read on. */
  timezone: string;
  /** The span, and the days of each of its buckets: one entry per entry of
   * every figure's buckets, whose own entries may be null and name no day. */
  span: PlatformTrendDays & { preset: PlatformTrendSpan; buckets: PlatformTrendDays[] };
  comparison: PlatformTrendDays & { kind: PlatformTrendComparison };
  granularity: PlatformTrendGranularity;
  today: string;
  figures: {
    tickets_sold: PlatformTrendFigure;
    money_sold: PlatformTrendFigure;
    platform_fee_earned: PlatformTrendFigure;
    new_verified_customers: PlatformTrendFigure;
    new_organizations: PlatformTrendFigure;
    page_views: PlatformTrendFigure;
    events_published: PlatformTrendFigure;
    active_organizations: PlatformTrendActivityFigure;
    active_customers: PlatformTrendActivityFigure;
  };
};

// ---------------------------------------------------------------------------
// The reading's address
// ---------------------------------------------------------------------------

/** The spans the operator chooses between, in the order the select offers them. */
export const PLATFORM_TREND_SPANS = ["7d", "30d", "90d", "12m", "custom"] as const;
export type PlatformTrendSpan = (typeof PLATFORM_TREND_SPANS)[number];

/** The comparisons: the span before, or the same span a year earlier. */
export const PLATFORM_TREND_COMPARISONS = ["previous", "year_earlier"] as const;
export type PlatformTrendComparison = (typeof PLATFORM_TREND_COMPARISONS)[number];

/**
 * Which reading is on screen. Every field is carried in the address, so a
 * reload or a link shared with another operator shows the same reading.
 * `from` and `to` are calendar days ("YYYY-MM-DD") for a custom span and ""
 * for every other.
 */
export type PlatformTrendsView = {
  span: PlatformTrendSpan;
  from: string;
  to: string;
  compare: PlatformTrendComparison;
};

/** What a bare /operator shows: 30 days against the span before. Never
 * written to the address, so the Overview's address stays the bare path. */
export const DEFAULT_PLATFORM_TRENDS_VIEW: PlatformTrendsView = {
  span: "30d",
  from: "",
  to: "",
  compare: "previous",
};

/** The raw query params the Overview reads, as Next hands them over: a
 * parameter written twice arrives as an array. */
export type PlatformTrendsSearchParams = {
  span?: string | string[];
  from?: string | string[];
  to?: string | string[];
  compare?: string | string[];
};

function first(raw: string | string[] | undefined): string {
  return ((Array.isArray(raw) ? raw[0] : raw) ?? "").trim();
}

const CALENDAR_DAY = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Whether a string is a day that exists on the calendar, spelled YYYY-MM-DD. */
function isCalendarDay(value: string): boolean {
  const match = CALENDAR_DAY.exec(value);
  if (!match) return false;
  const [year, month, day] = [Number(match[1]), Number(match[2]), Number(match[3])];
  const parsed = new Date(Date.UTC(year, month - 1, day));
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day;
}

/**
 * parsePlatformTrendsParams reads the reading off the address. A span or a
 * comparison this app does not know falls back to the default, each on its
 * own, and so does a custom span without two real calendar days: a mistyped
 * address still shows the operator a reading. What the two days of a custom
 * range may be (their order, how far they reach) is the API's to say, and its
 * refusal is shown in the section.
 */
export function parsePlatformTrendsParams(searchParams: PlatformTrendsSearchParams): PlatformTrendsView {
  const compareRaw = first(searchParams.compare) as PlatformTrendComparison;
  const compare = PLATFORM_TREND_COMPARISONS.includes(compareRaw)
    ? compareRaw
    : DEFAULT_PLATFORM_TRENDS_VIEW.compare;
  const spanRaw = first(searchParams.span) as PlatformTrendSpan;
  const span = PLATFORM_TREND_SPANS.includes(spanRaw) ? spanRaw : DEFAULT_PLATFORM_TRENDS_VIEW.span;
  if (span !== "custom") {
    return { span, from: "", to: "", compare };
  }
  const from = first(searchParams.from);
  const to = first(searchParams.to);
  if (!isCalendarDay(from) || !isCalendarDay(to)) {
    return { ...DEFAULT_PLATFORM_TRENDS_VIEW, compare };
  }
  return { span, from, to, compare };
}

/**
 * platformTrendsQuery is the canonical query string of a reading, with the
 * API's own parameter names. It is both the page's address and the request's
 * query, so the two cannot name different readings. The defaults are left
 * out. `parsePlatformTrendsParams` reads it back.
 */
export function platformTrendsQuery(view: PlatformTrendsView): string {
  const params = new URLSearchParams();
  if (view.span !== DEFAULT_PLATFORM_TRENDS_VIEW.span) params.set("span", view.span);
  if (view.span === "custom") {
    params.set("from", view.from);
    params.set("to", view.to);
  }
  if (view.compare !== DEFAULT_PLATFORM_TRENDS_VIEW.compare) params.set("compare", view.compare);
  const query = params.toString();
  return query ? `?${query}` : "";
}

/** The staff app's own route for the read, which forwards to the Go API. */
export const PLATFORM_TRENDS_API_PATH = "/api/operator/platform-trends";

/** The request a reading's address names. */
export function platformTrendsRequestPath(view: PlatformTrendsView): string {
  return `${PLATFORM_TRENDS_API_PATH}${platformTrendsQuery(view)}`;
}

/** fetchPlatformTrends reads Platform Trends through the BFF. Platform
 * Operator only: the Go API refuses anybody else. */
export async function fetchPlatformTrends(view: PlatformTrendsView): Promise<PlatformTrends> {
  return fetchEventsJSON<PlatformTrends>(platformTrendsRequestPath(view));
}

/**
 * The view after the operator chooses a span. A preset drops the custom days.
 * Custom starts from the span on screen, as the backend stated it, so the
 * first custom reading is the one already shown and the operator moves its
 * ends from there. With no reading on screen there are no days to start
 * from, and the view stays as it is.
 */
export function platformTrendsViewAfterSpanChange(
  view: PlatformTrendsView,
  span: PlatformTrendSpan,
  shown: PlatformTrendDays | null,
): PlatformTrendsView {
  if (span !== "custom") {
    return { span, from: "", to: "", compare: view.compare };
  }
  if (shown === null) {
    return view;
  }
  return { span, from: shown.first_day, to: shown.last_day, compare: view.compare };
}

/** The view after one day of the custom range is changed. A day emptied in
 * its input is not taken: a custom span always has both. */
export function platformTrendsViewAfterRangeChange(
  view: PlatformTrendsView,
  patch: { from?: string; to?: string },
): PlatformTrendsView {
  return {
    ...view,
    from: patch.from ? patch.from : view.from,
    to: patch.to ? patch.to : view.to,
  };
}

export function platformTrendsViewAfterComparisonChange(
  view: PlatformTrendsView,
  compare: PlatformTrendComparison,
): PlatformTrendsView {
  return { ...view, compare };
}

// ---------------------------------------------------------------------------
// Spelling
// ---------------------------------------------------------------------------

/**
 * A change as the tile states it: the backend's signed ratio as a signed
 * percentage, "+12.5%" and "-10%", with the reader's marks. Zero carries no
 * sign, and neither does a change too small to print. The tile hands this the
 * ratio it already rounded to one decimal of a percent, the precision kept
 * here, and reads its direction from that same number
 * (`roundedTrendChange` in the UI package), so the two agree. An absent
 * change never reaches here: it is null on the tile, which
 * says there is nothing to compare against.
 */
export function formatTrendChange(ratio: number, locale: AppLocale): string {
  return (
    new Intl.NumberFormat(staffIntlLocale(locale), {
      style: "percent",
      maximumFractionDigits: 1,
      signDisplay: "exceptZero",
    })
      .format(ratio)
      // Some locales sign a negative with U+2212; the tile's "-" is the plain
      // hyphen everywhere.
      .replace("\u2212", "-")
  );
}

/** A mean daily count, which is seldom whole: one decimal at most. */
export function formatTrendMean(value: number, locale: AppLocale): string {
  return formatNumber(value, locale, { maximumFractionDigits: 1 });
}

const BUCKET_DAY = { month: "short", day: "numeric", timeZone: "UTC" } as const;

function utcDay(day: string): Date | null {
  return isCalendarDay(day) ? new Date(`${day}T00:00:00Z`) : null;
}

/**
 * What a bucket is called on the axis and in the hover card: its day, or for
 * a bucket longer than a day its first and last ("Jun 29 - Jul 5", joined as
 * the reader's language joins a range).
 *
 * The days are the platform's, already resolved by the API, so they are read
 * and written in UTC and never moved into the reader's zone. No year, for the
 * reason `formatTrendsDay` gives: the span is stated beside the charts.
 */
export function formatTrendBucket(bucket: PlatformTrendDays, locale: AppLocale): string {
  const firstDay = utcDay(bucket.first_day);
  const lastDay = utcDay(bucket.last_day);
  if (firstDay === null || lastDay === null) {
    return bucket.first_day;
  }
  const format = new Intl.DateTimeFormat(staffIntlLocale(locale), BUCKET_DAY);
  return bucket.first_day === bucket.last_day ? format.format(firstDay) : format.formatRange(firstDay, lastDay);
}

// ---------------------------------------------------------------------------
// Response to chart input
// ---------------------------------------------------------------------------

/**
 * One bucket as a chart takes it. Structurally a `MultiSeriesLineDatum` of
 * the shared charts, and beside it the days the bucket covers, which is what
 * the recording-began marker is placed by.
 */
export type TrendChartDatum = {
  key: string;
  label: string;
  firstDay: string;
  lastDay: string;
  partial?: boolean;
  values: Record<string, number | null>;
  comparison?: Record<string, number | null>;
};

/** A bucket of a stacked chart: every value a number, and the stack's height,
 * or, before the figure's data began, every value and the height null. */
export type TrendStackDatum = TrendChartDatum & {
  values: Record<string, number | null>;
  total: number | null;
};

/** The id of the comparison line drawn for the series `key`. */
export function comparisonSeriesId(key: string): string {
  return `comparison:${key}`;
}

/** The id of the one comparison line a stack is set beside: the comparison
 * span's own stack height. */
export const STACK_COMPARISON_ID = "comparison:stack";

/** How the section names a bucket on the axis and in the hover card. */
export type BucketLabel = (bucket: PlatformTrendDays) => string;

/**
 * seriesChartData turns series into line chart buckets: each series' value in
 * each bucket, its comparison bucket's value beside it under
 * `comparisonSeriesId`, and today, where the response carries it, as a last
 * bucket marked partial with no comparison. A null bucket, before the
 * figure's data began, and a null comparison bucket stay null, which the
 * chart draws as a gap and never as a zero.
 *
 * The buckets are the span's (`spanBuckets`): a null bucket names no days of
 * its own, and a bucket holding the first day of data names only the days
 * from it, while the chart draws the whole bucket.
 */
export function seriesChartData(
  series: readonly PlatformTrendSeries[],
  span: readonly PlatformTrendDays[],
  label: BucketLabel,
): TrendChartDatum[] {
  const reference = series[0];
  if (reference === undefined) {
    return [];
  }
  const data: TrendChartDatum[] = span.map((bucket, index) => ({
    key: bucket.first_day,
    label: label(bucket),
    firstDay: bucket.first_day,
    lastDay: bucket.last_day,
    values: Object.fromEntries(series.map((entry) => [entry.key, entry.buckets[index]?.value ?? null])),
    comparison: Object.fromEntries(
      series.map((entry) => [comparisonSeriesId(entry.key), entry.comparison_buckets[index]?.value ?? null]),
    ),
  }));
  const today = reference.today;
  if (today !== null) {
    data.push({
      key: today.first_day,
      label: label(today),
      firstDay: today.first_day,
      lastDay: today.last_day,
      partial: today.partial,
      values: Object.fromEntries(series.map((entry) => [entry.key, entry.today?.value ?? null])),
    });
  }
  return data;
}

/**
 * stackedChartData is `seriesChartData` for a figure drawn as a stack
 * (Tickets Sold by Sales Channel): the same buckets, with the height of the
 * stack beside the values, and one comparison value, the height of the
 * comparison span's stack. That height is a gap where the comparison has no
 * data in any series, and never a zero. So is the stack's own: a bucket
 * before the figure's data began has no part and no height. The figure's
 * data begins on one day for every series, so a bucket is null in all of
 * them or in none.
 */
export function stackedChartData(
  series: readonly PlatformTrendSeries[],
  span: readonly PlatformTrendDays[],
  label: BucketLabel,
): TrendStackDatum[] {
  return seriesChartData(series, span, label).map((datum) => {
    const total = stackHeight(Object.values(datum.values));
    const values = Object.fromEntries(
      series.map((entry) => [entry.key, total === null ? null : (datum.values[entry.key] ?? 0)]),
    );
    const compared = Object.values(datum.comparison ?? {}).filter((value) => value !== null);
    return {
      ...datum,
      values,
      total,
      comparison:
        datum.comparison === undefined
          ? undefined
          : {
              [STACK_COMPARISON_ID]: compared.length === 0 ? null : compared.reduce((sum, value) => sum + value, 0),
            },
    };
  });
}

/**
 * The height of a stack of parts: their sum, or null when every part is, the
 * bucket being before the figure's data began. A missing part in a counted
 * bucket is a zero.
 */
function stackHeight(parts: readonly (number | null | undefined)[]): number | null {
  return parts.every((part) => part === null)
    ? null
    : parts.reduce<number>((sum, part) => sum + (part ?? 0), 0);
}

/**
 * The buckets of the span, which every chart is drawn on. A figure states one
 * entry per bucket of the span and null where it has no data (an activity
 * figure where no day is frozen, a count figure before its data began), so a
 * null entry has no days of its own; the span states them all.
 */
export function spanBuckets(trends: PlatformTrends): PlatformTrendDays[] {
  return trends.span.buckets;
}

/**
 * platformTrendsScrollAnchor names the data a reading draws, for its charts'
 * scroll areas to open at their newest end once per name (`openAtNewestEnd`
 * on `ChartScrollArea`). It is made of the span's days, the comparison's kind
 * and days, and the granularity, so another span, comparison or custom range
 * opens the charts on their latest days again, while the same reading
 * fetched again leaves a chart the Platform Operator scrolled where they
 * left it.
 */
export function platformTrendsScrollAnchor(trends: PlatformTrends): string {
  const { span, comparison, granularity } = trends;
  return [
    `${span.first_day}..${span.last_day}`,
    `${comparison.kind}:${comparison.first_day}..${comparison.last_day}`,
    granularity,
  ].join("|");
}

/** The one series an activity figure is drawn as. */
export const ACTIVITY_SERIES_ID = "all";

/**
 * activityChartData turns an activity figure into line chart buckets, one per
 * bucket of the span. A bucket with no frozen day is a null, which the chart
 * draws as a gap and, when every bucket is one, as "not recorded". There is
 * no today: the daily close freezes only days that have ended.
 */
export function activityChartData(
  figure: PlatformTrendActivityFigure,
  span: readonly PlatformTrendDays[],
  label: BucketLabel,
): TrendChartDatum[] {
  return figure.buckets.map((bucket, index) => {
    // The span's bucket names the whole bucket. Without one (the two lists
    // should never differ in length) the figure's own frozen days stand in.
    const whole = span[index] ?? bucket;
    const key = whole?.first_day ?? String(index);
    return {
      key,
      label: whole === null ? key : label(whole),
      firstDay: whole?.first_day ?? key,
      lastDay: whole?.last_day ?? key,
      values: { [ACTIVITY_SERIES_ID]: bucket?.value ?? null },
      comparison: {
        [comparisonSeriesId(ACTIVITY_SERIES_ID)]: figure.comparison_buckets[index]?.value ?? null,
      },
    };
  });
}

type DrawnBucket = Pick<TrendChartDatum, "key" | "firstDay" | "lastDay">;

/**
 * The key of the bucket the recording-began marker stands on: the one whose
 * days hold `day`. Null when the figure has no such day, or the chart does not
 * draw it (the span begins after it or ends before it), and then there is no
 * marker. Days are compared as the strings they are: YYYY-MM-DD sorts as the
 * calendar does.
 */
export function markerBucketKey(buckets: readonly DrawnBucket[], day: string | null): string | null {
  if (day === null) {
    return null;
  }
  return buckets.find((bucket) => bucket.firstDay <= day && day <= bucket.lastDay)?.key ?? null;
}

/**
 * Whether every bucket a chart draws lies before the day exact recording
 * began, so the chart has no marker to say it and the section says it in
 * words. False for a figure that was always exact (`exactSince` null) and for
 * a chart with no buckets.
 */
export function allBeforeMarker(exactSince: string | null, buckets: readonly DrawnBucket[]): boolean {
  const last = buckets.at(-1);
  return exactSince !== null && last !== undefined && exactSince > last.lastDay;
}

/**
 * Whether every bucket of the Active Organizations chart counts sales alone:
 * the backend says no day was ever frozen with a Member's work in it, or the
 * day that began is after the last bucket drawn.
 */
export function salesOnlyThroughout(
  figure: PlatformTrendActivityFigure,
  buckets: readonly DrawnBucket[],
): boolean {
  if (buckets.length === 0) {
    return false;
  }
  return figure.sale_half_only || figure.exact_since === null || allBeforeMarker(figure.exact_since, buckets);
}

/**
 * Whether the change of Active Organizations is stated against days that
 * count sales alone: the comparison span begins before the day a Member's
 * work was first counted. The change then sets fuller days against them,
 * which the section says beside the chart.
 *
 * With no change stated there is nothing for that note to qualify, and the
 * answer is no: the tile already says why it states no change.
 */
export function comparisonReachesSalesOnly(
  figure: PlatformTrendActivityFigure,
  comparison: PlatformTrendDays,
): boolean {
  if (figure.change === null) {
    return false;
  }
  return figure.exact_since === null || comparison.first_day < figure.exact_since;
}

/**
 * The top of a chart's Y axis: the tallest thing it draws, rounded up as
 * every trends chart rounds (`axisTopFor`). The tallest thing is a value, a
 * comparison value, or the height of a stack, so neither the comparison line
 * nor a stacked bar leaves the plot.
 */
export function trendChartYMax(data: readonly (TrendChartDatum & { total?: number | null })[]): number {
  const tallest = data.reduce(
    (max, datum) =>
      Math.max(
        max,
        datum.total ?? 0,
        ...Object.values(datum.values).map((value) => value ?? 0),
        ...Object.values(datum.comparison ?? {}).map((value) => value ?? 0),
      ),
    0,
  );
  return axisTopFor(tallest);
}

// ---------------------------------------------------------------------------
// Response to tile input
// ---------------------------------------------------------------------------

/**
 * Why a tile has no number to state, as a token the section puts into words
 * (lib/ returns tokens and is never handed copy): the figure is not recorded
 * at all, which is not a zero, or it is recorded and the span has nothing to
 * state.
 */
export type TrendTileAbsence = "notRecorded" | "noFigure";

/**
 * Why a reading states no change, as a token the section puts into words
 * (#837). The change is the backend's and is never worked out here; this only
 * says which of its reasons for leaving it out holds, read from what the
 * response already states:
 *
 * - `nothingToCompare`: the comparison span has no data at all (no comparison
 *   bucket, so no comparison total or mean), or nothing else below holds.
 * - `partlyRecorded`: the comparison span begins before the figure's
 *   data_since, so only its later buckets have data and the backend states no
 *   change from them (#826), whatever they come to.
 * - `noneCompared`: the comparison is recorded throughout and came to zero,
 *   and a change from zero has no ratio.
 */
export type TrendNoChange = "nothingToCompare" | "partlyRecorded" | "noneCompared";

/**
 * The reason a change is absent, or nothing when one is stated. `compared` is
 * the comparison's total, or an activity figure's comparison mean, as stated.
 */
function noChangeOf(
  change: number | null,
  compared: number | null,
  dataSince: string | null,
  comparison: PlatformTrendDays,
): { change: number | null; noChange?: TrendNoChange } {
  if (change !== null) {
    return { change };
  }
  if (compared === null || dataSince === null) {
    return { change, noChange: "nothingToCompare" };
  }
  if (comparison.first_day < dataSince) {
    return { change, noChange: "partlyRecorded" };
  }
  return { change, noChange: compared === 0 ? "noneCompared" : "nothingToCompare" };
}

/** One reading of a tile, before the section words it. */
export type TrendTileReadingInput = {
  id: string;
  /** The figure as spelled, or null, and then `absence` says why. */
  value: string | null;
  absence?: TrendTileAbsence;
  /** A currency code beside a money reading. Data, not copy. */
  currency?: string;
  /** True when the figure is a mean a day, which the section says beside it. */
  dailyMean?: boolean;
  /** Null states no change, and then `noChange` says why. Left out, the tile
   * states nothing about a change at all. */
  change?: number | null;
  noChange?: TrendNoChange;
  trend?: (number | null)[];
};

/** A lesser figure of a tile, named by its id: a series' key (a Sales
 * Channel), or `distinct_7d` and `distinct_30d`. The section owns the words.
 * `recordedDays` is set on a distinct count whose run of days is recorded
 * only in part, and says how many of its days are; the label then says so. */
export type TrendTileSecondaryInput = { id: string; value: string; recordedDays?: number };

export type TrendTileInput = {
  readings: TrendTileReadingInput[];
  secondary?: TrendTileSecondaryInput[];
};

const NOT_RECORDED_TILE: TrendTileInput = {
  readings: [{ id: "all", value: null, absence: "notRecorded" }],
};

/**
 * countTile is the tile of a figure that is one count: the total, the change
 * and the bucket-by-bucket shape, all as the backend states them. Today is in
 * no total and is left out of the trend line too.
 *
 * A figure split into several series (Tickets Sold by Sales Channel) draws
 * the height of its stack as the line, and with `splitBySeries` states each
 * series' own total beneath.
 */
export function countTile(
  figure: PlatformTrendFigure,
  comparison: PlatformTrendDays,
  format: (value: number) => string,
  { splitBySeries = false }: { splitBySeries?: boolean } = {},
): TrendTileInput {
  if (!figure.recorded) {
    return NOT_RECORDED_TILE;
  }
  const length = figure.series[0]?.buckets.length ?? 0;
  // A bucket before the figure's data began is null on every series, and a
  // gap in the line: nothing was counted there, which is not a zero.
  const trend = Array.from({ length }, (_, index) =>
    stackHeight(figure.series.map((entry) => entry.buckets[index]?.value ?? null)),
  );
  return {
    readings: [
      {
        id: "all",
        ...(figure.total === null
          ? { value: null, absence: "noFigure" as const }
          : { value: format(figure.total) }),
        ...noChangeOf(figure.change, figure.comparison_total, figure.data_since, comparison),
        trend,
      },
    ],
    ...(splitBySeries
      ? { secondary: figure.series.map((entry) => ({ id: entry.key, value: format(entry.total) })) }
      : {}),
  };
}

/**
 * moneyTile is the tile of a money figure: one reading per currency, each
 * with its own total, change and line, beside its currency code. No currency
 * is converted or added to another, so the tile has no figure across them.
 * Before any Organization exists there is no currency, and the tile has no
 * figure.
 */
export function moneyTile(
  figure: PlatformTrendFigure,
  comparison: PlatformTrendDays,
  format: (cents: number, currency: string) => string,
): TrendTileInput {
  if (!figure.recorded) {
    return NOT_RECORDED_TILE;
  }
  if (figure.series.length === 0) {
    return { readings: [{ id: "none", value: null, absence: "noFigure" }] };
  }
  return {
    readings: figure.series.map((entry) => ({
      id: entry.key,
      currency: entry.key,
      value: format(entry.total, entry.key),
      ...noChangeOf(entry.change, entry.comparison_total, figure.data_since, comparison),
      trend: entry.buckets.map((bucket) => bucket?.value ?? null),
    })),
  };
}

/**
 * activityTile is the tile of an activity figure. A count of different
 * subjects cannot be added across days, so the figure is the mean a day, with
 * its change, and beneath it the different subjects active over the last 7
 * and 30 days where the backend states them. A count whose days are not all
 * recorded carries how many are, since its label would otherwise claim the
 * whole run (#843).
 *
 * A figure that is not recorded says so and nothing else: no change, no line
 * and no counts, so nothing on the tile can be read as a zero.
 */
export function activityTile(
  figure: PlatformTrendActivityFigure,
  comparison: PlatformTrendDays,
  format: { mean: (value: number) => string; count: (value: number) => string },
): TrendTileInput {
  if (!figure.recorded) {
    return NOT_RECORDED_TILE;
  }
  const secondary: TrendTileSecondaryInput[] = [];
  const distinct = (id: string, value: number | null, recordedDays: number | null, windowDays: number) => {
    if (value === null) {
      return;
    }
    secondary.push({
      id,
      value: format.count(value),
      ...(recordedDays !== null && recordedDays < windowDays ? { recordedDays } : {}),
    });
  };
  distinct("distinct_7d", figure.distinct_7d, figure.distinct_7d_days, 7);
  distinct("distinct_30d", figure.distinct_30d, figure.distinct_30d_days, 30);
  return {
    readings: [
      {
        id: "all",
        ...(figure.mean === null
          ? { value: null, absence: "noFigure" as const }
          : { value: format.mean(figure.mean), dailyMean: true }),
        ...noChangeOf(figure.change, figure.comparison_mean, figure.data_since, comparison),
        trend: figure.buckets.map((bucket) => bucket?.value ?? null),
      },
    ],
    ...(secondary.length === 0 ? {} : { secondary }),
  };
}
