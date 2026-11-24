// Organization Trends (#861, #862, #863, #865, spec #858, ADR 0081): the Organization's figures
// over a chosen span, beneath what stands now on Home.
//
// The backend answers everything drawn: the buckets, which are partial, the
// totals, the comparison and the change. What is here is the little the page
// does with that answer: the reading's address (span and comparison, in the
// URL as the Operator Dashboard keeps them), the request that address names,
// the spelling of a month bucket, and the reshaping of the response into what
// the shared charts and the shared tile take. The address is read and
// written as Platform Trends' is (trends-span.ts), with an all time span
// besides, and the series are stated in the shape both readings share, so
// the reshaping helpers of trends-chart.ts are reused.
//
// The one place a number is added up here is the Cumulative view, which is a
// running sum of the buckets the backend stated, and the height of a stack.
// The Event table's rows are put in the API's order again on the page
// (organization-trends-events.ts), so its ranking follows the columns shown.
//
// Relative imports carry the ".ts" extension so the module graph resolves
// under `node --test` as well as the bundler.
import { fetchEventsJSON } from "./events-api.ts";
import { staffIntlLocale, type AppLocale } from "./format.ts";
import {
  formatTrendBucket,
  stackedChartData,
  type TrendStackDatum,
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
// The response, field for field as GET /api/v1/staff/organization/trends
// states it.
// ---------------------------------------------------------------------------

export type OrganizationTrendBucket = {
  first_day: string;
  last_day: string;
  value: number;
  /** Today, or a month the span cuts. */
  partial: boolean;
};

export type OrganizationTrendSeries = {
  /** The Sales Channel, or "all". */
  key: string;
  /** Over the span, today excluded. */
  total: number;
  buckets: OrganizationTrendBucket[];
  /** Today alone, partial and in no total; null when the span ends earlier. */
  today: OrganizationTrendBucket | null;
  /** Null for all time. */
  comparison_total: number | null;
  change: number | null;
  /** One entry per bucket; empty for all time. */
  comparison_buckets: OrganizationTrendBucket[];
};

export type OrganizationTrendFigure = {
  total: number;
  comparison_total: number | null;
  change: number | null;
  series: OrganizationTrendSeries[];
};

export type OrganizationTrendDays = TrendDays & { partial: boolean };

export type OrganizationTrendGranularity = "day" | "month";

export type OrganizationTrends = {
  timezone: string;
  today: string;
  currency: string;
  span: TrendDays & { preset: OrganizationTrendSpan; buckets: OrganizationTrendDays[] };
  /** Null for all time, which carries no comparison. */
  comparison: (TrendDays & { kind: OrganizationTrendComparison }) | null;
  granularity: OrganizationTrendGranularity;
  figures: {
    tickets_sold: OrganizationTrendFigure;
    ticket_sales: OrganizationTrendFigure;
    /** Loads of the Organization's Event pages, one series keyed all. Loads,
     * never people: nothing on Home is divided by it. */
    page_views: OrganizationTrendFigure;
    /** Published Events by the platform day each starts, one series keyed
     * all. */
    events_held: OrganizationTrendFigure;
    /**
     * Takings by Sales Channel, in the Organization's minor units, as Sales
     * Trends computes it (ADR 0040). The API withholds it from Event Staff
     * (#862): absent or null, never a zero, and the page then draws no
     * Takings at all.
     */
    takings?: OrganizationTrendFigure | null;
  };
  /**
   * The Event table (#865): every Event held, loaded or sold in the span,
   * today left out, ranked by the API. Its rows add up to the span's totals.
   */
  events: OrganizationTrendEvent[];
};

/** One row of the Event table: one Event's part of the span. */
export type OrganizationTrendEvent = {
  event_id: string;
  name: string;
  /** Null for an Event that names no start. */
  starts_at: string | null;
  /** The Event's own, its start printed in; "" where it names none. */
  timezone: string;
  tickets_sold: number;
  /** Minor units; withheld from Event Staff (null or absent), never zero. */
  takings_cents?: number | null;
  page_views: number;
};

// ---------------------------------------------------------------------------
// The reading's address
// ---------------------------------------------------------------------------

/** The spans a Member chooses between, in the order the select offers them. */
export const ORGANIZATION_TREND_SPANS = ["7d", "30d", "90d", "12m", "all", "custom"] as const;
export type OrganizationTrendSpan = (typeof ORGANIZATION_TREND_SPANS)[number];

/** The comparisons: the span before, or the same span a year earlier. */
export const ORGANIZATION_TREND_COMPARISONS = ["previous", "year"] as const;
export type OrganizationTrendComparison = (typeof ORGANIZATION_TREND_COMPARISONS)[number];

/**
 * Which reading is on screen, every field carried in the address so a reload
 * or a link sent to a colleague shows the same reading. `from` and `to` are
 * calendar days for a custom span and "" for every other. All time carries no
 * comparison, and its `compare` is the default, never written.
 */
export type OrganizationTrendsView = TrendSpanView<OrganizationTrendSpan, OrganizationTrendComparison>;

/** What a bare Home shows: 30 days against the span before. */
export const DEFAULT_ORGANIZATION_TRENDS_VIEW: OrganizationTrendsView = {
  span: "30d",
  from: "",
  to: "",
  compare: "previous",
};

/** The raw query params Home reads, as Next hands them over. */
export type OrganizationTrendsSearchParams = TrendSpanSearchParams;

/** Organization Trends' readings: Platform Trends' presets, all time
 * besides, which carries no comparison, and its own comparisons. */
export const ORGANIZATION_TRENDS_SPAN_SCHEME: TrendSpanScheme<OrganizationTrendSpan, OrganizationTrendComparison> = {
  spans: ORGANIZATION_TREND_SPANS,
  comparisons: ORGANIZATION_TREND_COMPARISONS,
  defaultView: DEFAULT_ORGANIZATION_TRENDS_VIEW,
  allTime: "all",
};

/** parseOrganizationTrendsParams reads the reading off the address
 * (`parseTrendSpanParams`). What a custom range may be beyond two real
 * calendar days is the API's to say. */
export function parseOrganizationTrendsParams(searchParams: OrganizationTrendsSearchParams): OrganizationTrendsView {
  return parseTrendSpanParams(ORGANIZATION_TRENDS_SPAN_SCHEME, searchParams);
}

/** organizationTrendsQuery is the canonical query string of a reading
 * (`trendSpanQuery`): both Home's address and the request's query, so the
 * two cannot name different readings. */
export function organizationTrendsQuery(view: OrganizationTrendsView): string {
  return trendSpanQuery(ORGANIZATION_TRENDS_SPAN_SCHEME, view);
}

/** The staff app's own route for the read, which forwards to the Go API. */
export const ORGANIZATION_TRENDS_API_PATH = "/api/settings/organization/trends";

export function organizationTrendsRequestPath(view: OrganizationTrendsView): string {
  return `${ORGANIZATION_TRENDS_API_PATH}${organizationTrendsQuery(view)}`;
}

/** fetchOrganizationTrends reads Organization Trends through the BFF. */
export async function fetchOrganizationTrends(view: OrganizationTrendsView): Promise<OrganizationTrends> {
  return fetchEventsJSON<OrganizationTrends>(organizationTrendsRequestPath(view));
}

/** The view after a span is chosen (`trendSpanViewAfterSpanChange`): all
 * time drops the comparison too. */
export function organizationTrendsViewAfterSpanChange(
  view: OrganizationTrendsView,
  span: OrganizationTrendSpan,
  shown: TrendDays | null,
): OrganizationTrendsView {
  return trendSpanViewAfterSpanChange(ORGANIZATION_TRENDS_SPAN_SCHEME, view, span, shown);
}

/** The view after one day of the custom range is changed; an emptied day is
 * not taken. */
export function organizationTrendsViewAfterRangeChange(
  view: OrganizationTrendsView,
  patch: { from?: string; to?: string },
): OrganizationTrendsView {
  return trendSpanViewAfterRangeChange(view, patch);
}

export function organizationTrendsViewAfterComparisonChange(
  view: OrganizationTrendsView,
  compare: OrganizationTrendComparison,
): OrganizationTrendsView {
  return trendSpanViewAfterComparisonChange(view, compare);
}

// ---------------------------------------------------------------------------
// Response to chart input
// ---------------------------------------------------------------------------

const MONTH = { month: "short", year: "numeric", timeZone: "UTC" } as const;
const CUT_MONTH = { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" } as const;

/**
 * What a bucket is called on the axis and in the hover card. A day is its day
 * and a whole month its month and year ("Jul 2026"); a month the span cuts
 * names its days ("Jul 1 - 6, 2026"), so the cut shows in the label. The
 * days are the platform's, read and written in UTC.
 */
export function formatOrganizationTrendBucket(
  bucket: OrganizationTrendDays,
  granularity: OrganizationTrendGranularity,
  locale: AppLocale,
): string {
  if (granularity !== "month" || bucket.first_day === bucket.last_day) {
    return formatTrendBucket(bucket, locale);
  }
  const firstDay = new Date(`${bucket.first_day}T00:00:00Z`);
  const lastDay = new Date(`${bucket.last_day}T00:00:00Z`);
  if (Number.isNaN(firstDay.getTime()) || Number.isNaN(lastDay.getTime())) {
    return bucket.first_day;
  }
  const intl = staffIntlLocale(locale);
  if (!bucket.partial) {
    return new Intl.DateTimeFormat(intl, MONTH).format(firstDay);
  }
  return new Intl.DateTimeFormat(intl, CUT_MONTH).formatRange(firstDay, lastDay);
}

/** How the section names a bucket. */
export type OrganizationBucketLabel = (bucket: OrganizationTrendDays) => string;

/**
 * The Daily view of a figure drawn as a stack (Tickets Sold by Sales
 * Channel): Platform Trends' stacked buckets, the comparison stack's height
 * beside each, today last and partial, and a month the span cuts flagged
 * partial too, as the backend flags it. Every bucket is counted: a bucket
 * that sold nothing is a zero.
 */
export function dailyStackData(
  figure: OrganizationTrendFigure,
  span: readonly OrganizationTrendDays[],
  label: OrganizationBucketLabel,
): TrendStackDatum[] {
  const partialByDay = new Map(span.map((bucket) => [bucket.first_day, bucket.partial]));
  return stackedChartData(figure.series, span, (bucket) =>
    label({ ...bucket, partial: partialByDay.get(bucket.first_day) ?? false }),
  ).map((datum) => (datum.partial || !partialByDay.get(datum.key) ? datum : { ...datum, partial: true }));
}

/**
 * The Cumulative view of a stacked figure: each Sales Channel's running sum
 * from zero at the span's start, bucket by bucket, so the last bucket stands
 * at the span's total. Today is in no total, so the view ends at yesterday;
 * it has no comparison line.
 */
export function cumulativeStackData(
  figure: OrganizationTrendFigure,
  span: readonly OrganizationTrendDays[],
  label: OrganizationBucketLabel,
): TrendStackDatum[] {
  const running = new Map<string, number>(figure.series.map((entry) => [entry.key, 0]));
  return span.map((bucket, index) => {
    const values: Record<string, number> = {};
    for (const entry of figure.series) {
      const sum = (running.get(entry.key) ?? 0) + (entry.buckets[index]?.value ?? 0);
      running.set(entry.key, sum);
      values[entry.key] = sum;
    }
    return {
      key: bucket.first_day,
      label: label(bucket),
      firstDay: bucket.first_day,
      lastDay: bucket.last_day,
      values,
      total: Object.values(values).reduce((sum, value) => sum + value, 0),
    };
  });
}

// ---------------------------------------------------------------------------
// Response to tile input
// ---------------------------------------------------------------------------

/**
 * Why a reading states no change, as a token the section words: there is a
 * comparison and it came to zero (`noneCompared`). All time states nothing
 * about a change at all, and its tile has none.
 */
export type OrganizationTrendNoChange = "noneCompared";

/**
 * The tile of a figure: its total, its change against the comparison span
 * (left out for all time, which has none) and the shape of the span, the
 * stack's height bucket by bucket. Today is in neither.
 */
export function organizationTrendTile(
  figure: OrganizationTrendFigure,
  compared: boolean,
  format: (value: number) => string,
): TrendTileReadingInput & { noChange?: OrganizationTrendNoChange } {
  const length = figure.series[0]?.buckets.length ?? 0;
  const trend = Array.from({ length }, (_, index) =>
    figure.series.reduce((sum, entry) => sum + (entry.buckets[index]?.value ?? 0), 0),
  );
  if (!compared) {
    return { id: "all", value: format(figure.total), trend };
  }
  return {
    id: "all",
    value: format(figure.total),
    change: figure.change,
    ...(figure.change === null && figure.comparison_total === 0 ? { noChange: "noneCompared" as const } : {}),
    trend,
  };
}

/**
 * organizationTrendsScrollAnchor names the data a reading draws, so its charts
 * open at their newest end once per reading and view.
 */
export function organizationTrendsScrollAnchor(trends: OrganizationTrends, view: string): string {
  const { span, comparison, granularity } = trends;
  return [
    `${span.first_day}..${span.last_day}`,
    comparison === null ? "none" : `${comparison.kind}:${comparison.first_day}..${comparison.last_day}`,
    granularity,
    view,
  ].join("|");
}

/** Whether a figure counted nothing at all over the span or today (nothing
 * sold, loaded or held), which the section says in words beside the zeros. */
export function countedNothing(figure: OrganizationTrendFigure): boolean {
  return figure.total === 0 && figure.series.every((entry) => (entry.today?.value ?? 0) === 0);
}
