// A trends reading reshaped for what the shared charts and the shared tile
// take, as every trends surface of the staff app draws it: Platform Trends on
// the Operator Dashboard's Overview and Organization Trends on Home alike.
//
// Both readings state their series in one shape (`TrendSeriesInput`): one
// entry per bucket of the span, the comparison span's bucket beside each,
// and today apart, partial and in no total. Everything drawn is the
// backend's; the one place a number is added up here is the height of a
// stack. What each surface adds of its own (Platform Trends' recording
// markers and activity figures, Organization Trends' month buckets and
// Cumulative view) stays in its own module.
//
// Relative imports carry the ".ts" extension so the module graph resolves
// under `node --test` as well as the bundler.
import { staffIntlLocale, type AppLocale } from "./format.ts";
import { axisTopFor } from "./sales-trends.ts";
import { isCalendarDay, type TrendDays } from "./trends-span.ts";

/** One bucket of a series: a day, or the days of a longer bucket, both ends
 * included. */
export type TrendBucketInput = TrendDays & {
  /** A count, or for money an amount in minor units. */
  value: number;
  /** True on a bucket still running (today), or one the span cuts. */
  partial: boolean;
};

/** One series of a figure as both readings state it. */
export type TrendSeriesInput = {
  /** The Sales Channel, the currency code, or "all". */
  key: string;
  /** One entry per bucket of the span, null where nothing was counted, which
   * is not a zero. */
  buckets: readonly (TrendBucketInput | null)[];
  /** Today, in no total. Null when the span ends earlier. */
  today: TrendBucketInput | null;
  /** One entry per entry of `buckets`, null (or absent) where the comparison
   * has no data. */
  comparison_buckets: readonly (TrendBucketInput | null)[];
};

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
export function formatTrendBucket(bucket: TrendDays, locale: AppLocale): string {
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
export type BucketLabel = (bucket: TrendDays) => string;

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
  series: readonly TrendSeriesInput[],
  span: readonly TrendDays[],
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
  series: readonly TrendSeriesInput[],
  span: readonly TrendDays[],
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
export function stackHeight(parts: readonly (number | null | undefined)[]): number | null {
  return parts.every((part) => part === null)
    ? null
    : parts.reduce<number>((sum, part) => sum + (part ?? 0), 0);
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

// ---------------------------------------------------------------------------
// Choosing a bucket
// ---------------------------------------------------------------------------

/**
 * The key of the bucket selected on one figure's chart, or null: the
 * selection is another figure's, or the chart does not draw the bucket, and
 * then it marks none.
 */
export function bucketSelectionIn<Figure extends string>(
  selection: { figure: Figure; key: string } | null,
  figure: Figure,
  buckets: readonly { key: string }[],
): string | null {
  if (selection === null || selection.figure !== figure) {
    return null;
  }
  return buckets.some((bucket) => bucket.key === selection.key) ? selection.key : null;
}
