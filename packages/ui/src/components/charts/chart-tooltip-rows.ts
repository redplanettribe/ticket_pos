/**
 * Which of a line chart's series the hover card names for one bucket.
 *
 * A plain module beside `chart-geometry.ts` for the same reason: the rule must
 * be testable under `node --test`, which cannot parse JSX, and nothing in it
 * depends on React. `MultiSeriesLineTooltip` renders what this returns.
 */

import type { ChartTooltipRow, StackedSeries } from "./chart-frame";
import type { ChartBucketReading } from "./chart-reading";

/**
 * How the chart reads a zero. On a counting view (Clicks, Attributed Sales) a
 * zero is the absence of anything to count, and says no more than a gap
 * would. On a measured view (Attribution Rate) a zero is a finding — clicks
 * came and nobody bought — and the chart must say so, or a 0% would be
 * indistinguishable from the gap a no-click day draws.
 *
 * Stated by the chart rather than inferred from the bucket, because the bucket
 * cannot tell the two apart: the sales view states a detail ("0 tickets")
 * under a zero just as the rate view does under a 0%.
 */
export type TooltipRowOptions = {
  zeroIsMeasured: boolean;
};

/**
 * The rows a bucket's hover card shows, in the order the chart draws the
 * series, and whether there are none.
 *
 * A series with nothing to say in the bucket is not a row: a null is the gap
 * in the line, and a zero on a counting view is a count of nothing. A card for
 * an Event with thirty links would otherwise name thirty series for an hour in
 * which two of them counted anything, and the reader would hunt for the two.
 * A zero on a measured view (`zeroIsMeasured`) is kept, with its detail.
 *
 * `nothingCounted` is true exactly when every series dropped — the card then
 * says so in one line rather than drawing an empty list, which reads as a
 * rendering fault.
 *
 * `notRecorded` narrows that: true when every series dropped because every
 * one of them is a gap. Nothing was measured in the bucket at all, which a
 * chart that has the words for it says instead of "nothing counted" (see
 * `tooltipEmptyLabel`).
 */
export function tooltipRows(
  series: readonly StackedSeries[],
  values: Readonly<Record<string, number | null | undefined>>,
  formatValue: (value: number) => string,
  formatDetail: (seriesId: string) => string | null,
  { zeroIsMeasured }: TooltipRowOptions,
): { rows: ChartTooltipRow[]; nothingCounted: boolean; notRecorded: boolean } {
  const rows: ChartTooltipRow[] = [];
  let measured = false;
  for (const entry of series) {
    const value = values[entry.id] ?? null;
    measured ||= value !== null;
    if (value === null || (value === 0 && !zeroIsMeasured)) {
      continue;
    }
    rows.push({
      id: entry.id,
      name: entry.name,
      color: entry.color,
      value: formatValue(value),
      detail: formatDetail(entry.id),
    });
  }
  return { rows, nothingCounted: rows.length === 0, notRecorded: !measured };
}

/**
 * The one muted line a hover card says in place of its rows, or undefined when
 * it has rows to show or the chart was given no words for having none.
 *
 * "Not recorded" extends "nothing counted" and does not sit beside it: a
 * bucket that was not recorded also counted nothing, so a chart without
 * `notRecordedLabel` says of it what it always said, and a chart with it says
 * the more exact thing.
 */
export function tooltipEmptyLabel(
  reading: { nothingCounted: boolean; notRecorded: boolean },
  labels: { nothingCountedLabel?: string; notRecordedLabel?: string },
): string | undefined {
  if (!reading.nothingCounted) {
    return undefined;
  }
  if (reading.notRecorded && labels.notRecordedLabel !== undefined) {
    return labels.notRecordedLabel;
  }
  return labels.nothingCountedLabel;
}

/**
 * The rows a bucket's hover card adds for the comparison series, in the order
 * the chart draws them and after the series' own rows.
 *
 * A bucket the comparison does not reach has no row, as it has no point on the
 * line. A zero is a row whatever the chart makes of its own zeros: the reader
 * is setting two spans side by side, and "the span before came to nothing" is
 * the answer. A row is marked `faint`, so the card draws its swatch the way
 * the chart draws its line, and its id is prefixed so a comparison series may
 * share an id with the series it is compared with.
 */
export function comparisonTooltipRows(
  comparisonSeries: readonly StackedSeries[] | undefined,
  bucket: ChartBucketReading,
  formatValue: (value: number) => string,
): ChartTooltipRow[] {
  const rows: ChartTooltipRow[] = [];
  for (const entry of comparisonSeries ?? []) {
    // Read as `comparisonValue` reads it. Spelled out here because this module
    // runs under `node --test`, where one plain module cannot import another
    // without naming its extension, which the bundler's resolution forbids.
    const value = bucket.comparison?.[entry.id] ?? null;
    if (value === null) {
      continue;
    }
    rows.push({
      id: `comparison:${entry.id}`,
      name: entry.name,
      color: entry.color,
      value: formatValue(value),
      faint: true,
    });
  }
  return rows;
}

/**
 * The muted lines a hover card states under its bucket label, before any row.
 * Today there is one: a partial bucket says it is partial, in the words the
 * chart was given, so a figure still being counted is not read as a collapse.
 */
export function tooltipNotes(bucket: ChartBucketReading, labels: { partialLabel?: string }): string[] {
  return bucket.partial === true && labels.partialLabel !== undefined ? [labels.partialLabel] : [];
}
