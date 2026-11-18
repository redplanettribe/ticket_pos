"use client";

import * as React from "react";
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from "recharts";

import {
  CHART_MARGIN,
  ChartTooltipCard,
  StackedChartShell,
  TOOLTIP_PROPS,
  Y_AXIS_PROPS,
  xAxisProps,
  selectableChartProps,
  type StackedSeries,
} from "./chart-frame";
import { nothingRecorded, type ChartBucketReading } from "./chart-reading";
import {
  comparisonLines,
  markerLine,
  partialBarCells,
  selectionMarks,
  useBucketSelection,
  type ChartReadingProps,
} from "./chart-reading-marks";
import { comparisonTooltipRows, tooltipEmptyLabel, tooltipNotes } from "./chart-tooltip-rows";

/**
 * The widest a single grouped bar is drawn. Narrower than the stacked chart's
 * cap because a bucket here holds one bar PER series side by side, and a few
 * series of slabs would leave no bucket visible between them.
 */
const MAX_BAR_SIZE = 24;

/**
 * One bucket of a multi-series chart: its key, its axis label, and the value
 * each series contributed.
 *
 * Deliberately not `StackedDatum`: there is no `total`, because the series here
 * are not parts of a whole. The chart this was built for draws each Affiliate
 * Link's Clicks BESIDE the Event's whole Page Views — a superset, not a sibling
 * — and a summed tooltip row would add a link's clicks to the page views they
 * are already inside, which is a number that means nothing. A datum that cannot
 * carry a total cannot be asked to lie with one.
 *
 * A value may be null, for a bucket in which the series was not recorded. It
 * draws no bar, as a zero does, and is read differently: see
 * `notRecordedLabel` on `ChartReadingProps`.
 */
export type MultiSeriesDatum = ChartBucketReading & {
  key: string;
  label: string;
  values: Record<string, number | null>;
};

/** What a multi-series bar chart is told. See `StackedChartProps` for why the
 * axis geometry (`yMax`, `yTicks`, `plotWidth`) is owned by the caller, and
 * `ChartReadingProps` for what a trend reading adds. */
export type MultiSeriesBarChartProps = ChartReadingProps & {
  data: MultiSeriesDatum[];
  series: StackedSeries[];
  yMax: number;
  yTicks?: number[];
  plotWidth: number;
  formatValue: (value: number) => string;
  formatTickValue?: (value: number) => string;
  /**
   * An extra muted line under a series' tooltip row — the Attributed Sales
   * view states each link's tickets and Net Proceeds there. Null for "this
   * series has nothing further to say"; omitted when no view on the surface
   * does.
   */
  formatSeriesDetail?: (seriesId: string, datum: MultiSeriesDatum) => string | null;
  syncId?: string;
  height?: number;
  ariaLabel: string;
  className?: string;
};

/**
 * MultiSeriesBarChart draws each series as its own bar within every bucket —
 * grouped, never stacked.
 *
 * It exists beside `StackedBarChart` rather than as a mode of it because the two
 * answer different questions about the same shape of data. Stacking says "these
 * categories are parts of one figure"; grouping says "these figures are to be
 * compared". Affiliate Link trends are the second kind: one link's Clicks
 * against another's, and both against the whole page's views — which CONTAIN the
 * clicks, so stacking them would double-count by construction.
 *
 * Like every chart on a surface, it is measure-agnostic and draws inside the
 * shared `chart-frame` geometry, so it lines up with any stacked chart placed in
 * the same scroll area.
 */
export function MultiSeriesBarChart({
  data,
  series,
  yMax,
  yTicks,
  plotWidth,
  formatValue,
  formatTickValue,
  formatSeriesDetail,
  syncId,
  height = 320,
  ariaLabel,
  className,
  comparisonSeries,
  partialLabel,
  marker,
  selection,
  notRecordedLabel,
}: MultiSeriesBarChartProps) {
  const formatTick = formatTickValue ?? formatValue;
  const labels = data.map((datum) => datum.label);
  const { overlay, selectedIndex, cursorIndex, onChartClick } = useBucketSelection(data, selection, {
    plotWidth,
    height,
    shape: "band",
  });
  const unrecorded = nothingRecorded(
    data,
    series.map((entry) => entry.id),
  );
  return (
    <StackedChartShell
      plotWidth={plotWidth}
      height={height}
      yMax={yMax}
      yTicks={yTicks}
      formatTick={formatTick}
      ariaLabel={ariaLabel}
      className={className}
      overlay={overlay}
      notRecordedLabel={unrecorded ? notRecordedLabel : undefined}
    >
      {(width) => (
        <BarChart
          width={width}
          height={height}
          data={data}
          syncId={syncId}
          margin={CHART_MARGIN}
          {...selectableChartProps(onChartClick)}
        >
          <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border" />
          <XAxis {...xAxisProps(labels, plotWidth)} />
          <YAxis {...Y_AXIS_PROPS} domain={[0, yMax]} ticks={yTicks} tickFormatter={formatTick} />
          <Tooltip
            {...TOOLTIP_PROPS}
            cursor={{ className: "fill-muted", opacity: 0.4 }}
            content={
              <MultiSeriesChartTooltip
                series={series}
                formatValue={formatValue}
                formatSeriesDetail={formatSeriesDetail}
                chartHeight={height}
                comparisonSeries={comparisonSeries}
                partialLabel={partialLabel}
                notRecordedLabel={notRecordedLabel}
              />
            }
          />
          {selectionMarks(data, { selectedIndex, cursorIndex }, "band")}
          {series.map((entry) => (
            <Bar
              key={entry.id}
              // A function reads the value out of `values` so a series id can be
              // any string (an Affiliate Link's UUID) without colliding with the
              // datum's own fields.
              dataKey={(datum: MultiSeriesDatum) => datum.values[entry.id] ?? 0}
              name={entry.name}
              fill={entry.color}
              maxBarSize={MAX_BAR_SIZE}
              isAnimationActive={false}
            >
              {partialBarCells(data, entry.color)}
            </Bar>
          ))}
          {comparisonLines(comparisonSeries)}
          {markerLine(data, marker, "band")}
        </BarChart>
      )}
    </StackedChartShell>
  );
}

type MultiSeriesChartTooltipProps = {
  series: StackedSeries[];
  formatValue: (value: number) => string;
  formatSeriesDetail?: (seriesId: string, datum: MultiSeriesDatum) => string | null;
  /** The chart's height, handed on so the card can fit itself to it. */
  chartHeight: number;
  comparisonSeries?: StackedSeries[];
  partialLabel?: string;
  notRecordedLabel?: string;
  /** Injected by recharts when it clones this element. */
  active?: boolean;
  payload?: { payload?: MultiSeriesDatum }[];
};

/**
 * The hover card for one bucket: every drawn series with its exact figure, in
 * the caller's series order, zeros stated rather than omitted — and no total
 * row, for the reason `MultiSeriesDatum` has no total.
 *
 * A series that was not recorded in the bucket (a null) has no row, since it
 * has no figure and a zero would invent one; when that leaves the card with
 * nothing, it says so in the chart's words for it. A partial bucket says so
 * first, and the comparison series' rows follow the series' own.
 */
export function MultiSeriesChartTooltip({
  series,
  formatValue,
  formatSeriesDetail,
  chartHeight,
  comparisonSeries,
  partialLabel,
  notRecordedLabel,
  active,
  payload,
}: MultiSeriesChartTooltipProps) {
  const datum = payload?.[0]?.payload;
  if (!active || !datum) {
    return null;
  }
  const rows = series
    // An absent value is the zero it always was; only a stated null is "not
    // recorded".
    .filter((entry) => datum.values[entry.id] !== null)
    .map((entry) => ({
      id: entry.id,
      name: entry.name,
      color: entry.color,
      value: formatValue(datum.values[entry.id] ?? 0),
      detail: formatSeriesDetail?.(entry.id, datum) ?? null,
    }));
  const comparisonRows = comparisonTooltipRows(comparisonSeries, datum, formatValue);
  return (
    <ChartTooltipCard
      label={datum.label}
      chartHeight={chartHeight}
      rows={[...rows, ...comparisonRows]}
      notes={tooltipNotes(datum, { partialLabel })}
      emptyLabel={
        comparisonRows.length === 0
          ? tooltipEmptyLabel(
              { nothingCounted: rows.length === 0, notRecorded: series.length > 0 },
              { notRecordedLabel },
            )
          : undefined
      }
    />
  );
}
