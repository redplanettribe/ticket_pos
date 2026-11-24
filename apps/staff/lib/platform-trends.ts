// Platform Trends (#821, spec #808, ADR 0080): the growth reading at the top
// of the Operator Dashboard's Overview.
//
// The backend answers everything the section draws: the buckets, the totals,
// the change against the comparison span, which bucket is partial, and the day
// exact recording began. What is here is the little the page does with that
// answer before drawing it: the reading's address (span and comparison, in the
// URL as the Tax Invoices list keeps its filters), the request that address
// names, the spelling of a mean, and the reshaping of the response into what
// the shared charts and the shared tile take. What Organization Trends reads
// alike is shared: the address in trends-span.ts, the spelling of a change
// and of a bucket and the series' reshaping in trends-chart.ts.
//
// Nothing here buckets, totals a span, compares two spans or decides what is
// partial. The two places a number is added up are drawing a stack: the height
// of the Tickets Sold bar and of the line it is compared with, which is what a
// stacked chart is.
//
// Relative imports carry the ".ts" extension so the module graph resolves
// under `node --test` as well as the bundler.
import { fetchEventsJSON } from "./events-api.ts";
import { formatNumber, type AppLocale } from "./format.ts";
import {
  comparisonSeriesId,
  stackHeight,
  type BucketLabel,
  type TrendChartDatum,
  type TrendNoChange,
  type TrendTileReadingInput,
} from "./trends-chart.ts";
import {
  parseTrendSpanParams,
  trendSpanQuery,
  trendSpanViewAfterComparisonChange,
  trendSpanViewAfterRangeChange,
  trendSpanViewAfterSpanChange,
  type TrendDays,
  type TrendSpanScheme,
  type TrendSpanSearchParams,
  type TrendSpanView,
} from "./trends-span.ts";

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

export type PlatformTrendDays = TrendDays;

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
export type PlatformTrendsView = TrendSpanView<PlatformTrendSpan, PlatformTrendComparison>;

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
export type PlatformTrendsSearchParams = TrendSpanSearchParams;

/** Platform Trends' readings: its presets and comparisons, and no all time
 * span, which only Organization Trends offers. */
export const PLATFORM_TRENDS_SPAN_SCHEME: TrendSpanScheme<PlatformTrendSpan, PlatformTrendComparison> = {
  spans: PLATFORM_TREND_SPANS,
  comparisons: PLATFORM_TREND_COMPARISONS,
  defaultView: DEFAULT_PLATFORM_TRENDS_VIEW,
  allTime: null,
};

/** parsePlatformTrendsParams reads the reading off the address
 * (`parseTrendSpanParams`). */
export function parsePlatformTrendsParams(searchParams: PlatformTrendsSearchParams): PlatformTrendsView {
  return parseTrendSpanParams(PLATFORM_TRENDS_SPAN_SCHEME, searchParams);
}

/** platformTrendsQuery is the canonical query string of a reading
 * (`trendSpanQuery`): both the page's address and the request's query. */
export function platformTrendsQuery(view: PlatformTrendsView): string {
  return trendSpanQuery(PLATFORM_TRENDS_SPAN_SCHEME, view);
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

/** The view after the operator chooses a span (`trendSpanViewAfterSpanChange`). */
export function platformTrendsViewAfterSpanChange(
  view: PlatformTrendsView,
  span: PlatformTrendSpan,
  shown: PlatformTrendDays | null,
): PlatformTrendsView {
  return trendSpanViewAfterSpanChange(PLATFORM_TRENDS_SPAN_SCHEME, view, span, shown);
}

/** The view after one day of the custom range is changed. A day emptied in
 * its input is not taken: a custom span always has both. */
export function platformTrendsViewAfterRangeChange(
  view: PlatformTrendsView,
  patch: { from?: string; to?: string },
): PlatformTrendsView {
  return trendSpanViewAfterRangeChange(view, patch);
}

export function platformTrendsViewAfterComparisonChange(
  view: PlatformTrendsView,
  compare: PlatformTrendComparison,
): PlatformTrendsView {
  return trendSpanViewAfterComparisonChange(view, compare);
}

// ---------------------------------------------------------------------------
// Spelling
// ---------------------------------------------------------------------------

/** A mean daily count, which is seldom whole: one decimal at most. */
export function formatTrendMean(value: number, locale: AppLocale): string {
  return formatNumber(value, locale, { maximumFractionDigits: 1 });
}

// ---------------------------------------------------------------------------
// Response to chart input
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Response to tile input
// ---------------------------------------------------------------------------

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

/** A lesser figure of a tile, named by its id: a series' key (a Sales
 * Channel), or `distinct_7d` and `distinct_30d`. The section owns the words.
 * `recordedDays` is set on a distinct count whose run of days is recorded
 * only in part, and says how many of its days are; the label then says so. */
export type TrendTileSecondaryInput = { id: string; value: string; recordedDays?: number };

export type TrendTileInput = {
  readings: TrendTileReadingInput[];
  secondary?: TrendTileSecondaryInput[];
};

/** The runs of days the distinct counts are over, as the backend's daily
 * close freezes them. */
const DISTINCT_SHORT_DAYS = 7;
const DISTINCT_LONG_DAYS = 30;

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
  distinct("distinct_7d", figure.distinct_7d, figure.distinct_7d_days, DISTINCT_SHORT_DAYS);
  distinct("distinct_30d", figure.distinct_30d, figure.distinct_30d_days, DISTINCT_LONG_DAYS);
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
