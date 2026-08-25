/**
 * Which of a line chart's series the hover card names for one bucket.
 *
 * A plain module beside `chart-geometry.ts` for the same reason: the rule must
 * be testable under `node --test`, which cannot parse JSX, and nothing in it
 * depends on React. `MultiSeriesLineTooltip` renders what this returns.
 */

import type { ChartTooltipRow, StackedSeries } from "./chart-frame";

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
 */
export function tooltipRows(
  series: readonly StackedSeries[],
  values: Readonly<Record<string, number | null | undefined>>,
  formatValue: (value: number) => string,
  formatDetail: (seriesId: string) => string | null,
  { zeroIsMeasured }: TooltipRowOptions,
): { rows: ChartTooltipRow[]; nothingCounted: boolean } {
  const rows: ChartTooltipRow[] = [];
  for (const entry of series) {
    const value = values[entry.id] ?? null;
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
  return { rows, nothingCounted: rows.length === 0 };
}
