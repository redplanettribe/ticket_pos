"use client";

import * as React from "react";
import { CartesianGrid, Line, LineChart, Tooltip, XAxis, YAxis } from "recharts";

import {
  CHART_MARGIN,
  ChartTooltipCard,
  StackedChartShell,
  selectableChartProps,
  TOOLTIP_PROPS,
  Y_AXIS_PROPS,
  xAxisProps,
  type StackedSeries,
} from "./chart-frame";
import { nothingRecorded, partialLineValues, type ChartBucketReading } from "./chart-reading";
import {
  comparisonLines,
  isolatedPointDot,
  markerLine,
  partialStretchLines,
  selectionMarks,
  useBucketSelection,
  type ChartReadingProps,
} from "./chart-reading-marks";
import { comparisonTooltipRows, tooltipEmptyLabel, tooltipNotes, tooltipRows } from "./chart-tooltip-rows";

/**
 * One bucket of a multi-series LINE chart: its key, its axis label, and each
 * series' value there — or null, for "this series has nothing to say here".
 *
 * The null is the whole reason this datum is not `MultiSeriesDatum`. A line
 * states a quantity that exists continuously; where the quantity is undefined —
 * a rate whose denominator was zero — the only honest drawing is a gap, and a
 * gap must be representable in the data or the chart would be forced to draw a
 * zero that means something else entirely.
 */
export type MultiSeriesLineDatum = ChartBucketReading & {
  key: string;
  label: string;
  values: Record<string, number | null>;
};

/** What a multi-series line chart is told. See `StackedChartProps` for why the
 * axis geometry (`yMax`, `yTicks`, `plotWidth`) is owned by the caller, and
 * `ChartReadingProps` for what a trend reading adds. */
export type MultiSeriesLineChartProps = ChartReadingProps & {
  data: MultiSeriesLineDatum[];
  series: StackedSeries[];
  yMax: number;
  yTicks?: number[];
  plotWidth: number;
  formatValue: (value: number) => string;
  formatTickValue?: (value: number) => string;
  /** An extra muted line under a series' tooltip row — the Attribution Rate
   * view states the division behind each point there. */
  formatSeriesDetail?: (seriesId: string, datum: MultiSeriesLineDatum) => string | null;
  /** Whether a zero is a finding the hover card names (a 0% rate: clicks came,
   * nobody bought) or a count of nothing it leaves out. Off by default, which
   * is what a counting view means by zero. See `tooltipRows`. */
  zeroIsMeasured?: boolean;
  /** What the hover card says for a bucket in which no drawn series counted
   * anything. Passed in by the surface, which owns its words. A chart also
   * given `notRecordedLabel` says that instead where every series is a gap. */
  nothingCountedLabel: string;
  syncId?: string;
  height?: number;
  ariaLabel: string;
  className?: string;
};

/**
 * MultiSeriesLineChart draws each series as its own line — never stacked,
 * never filled, and broken where a value is null. A value with a gap on both
 * sides is drawn as a dot, since no segment can reach it (see
 * `isolatedPointDot`).
 *
 * It exists beside `MultiSeriesBarChart` for the same reason that exists beside
 * `StackedBarChart`: the shape carries the meaning. Bars are for counts that
 * stand alone; a line is for a figure read as a trajectory, like a rate, where
 * the reader follows how it moves rather than how tall any point is. Straight
 * segments, not splines — a curve smoothed through real points invents values
 * between them (see StackedAreaChart's reasoning).
 *
 * It draws inside the shared `chart-frame` geometry, so it lines up with any
 * other chart placed in the same scroll area.
 */
export function MultiSeriesLineChart({
  data,
  series,
  yMax,
  yTicks,
  plotWidth,
  formatValue,
  formatTickValue,
  formatSeriesDetail,
  zeroIsMeasured = false,
  nothingCountedLabel,
  syncId,
  height = 320,
  ariaLabel,
  className,
  comparisonSeries,
  partialLabel,
  marker,
  selection,
  notRecordedLabel,
}: MultiSeriesLineChartProps) {
  const formatTick = formatTickValue ?? formatValue;
  const labels = data.map((datum) => datum.label);
  const seriesIds = series.map((entry) => entry.id);
  const { overlay, selectedIndex, cursorIndex, onChartClick } = useBucketSelection(data, selection, {
    plotWidth,
    height,
    shape: "point",
  });
  // A chart with a partial bucket draws each series twice: the closed line,
  // which gives that bucket up, and the dashed stretch that reaches it. The
  // split rides on a copy of each datum, so the hover card still reads the
  // caller's own fields. Without a partial bucket the data is drawn as given.
  const drawn: DrawnLineDatum[] = React.useMemo(() => {
    if (!data.some((datum) => datum.partial === true)) {
      return data;
    }
    const partial = data.map((datum) => datum.partial === true);
    const rows: DrawnLineDatum[] = data.map((datum) => ({ ...datum, closed: {}, stretch: {} }));
    for (const { id } of series) {
      const split = partialLineValues(
        data.map((datum) => datum.values[id] ?? null),
        partial,
      );
      rows.forEach((row, index) => {
        row.closed![id] = split.closed[index] ?? null;
        row.stretch![id] = split.partial[index] ?? null;
      });
    }
    return rows;
  }, [data, series]);
  const hasPartial = drawn !== data;
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
      notRecordedLabel={nothingRecorded(data, seriesIds) ? notRecordedLabel : undefined}
    >
      {(width) => (
        <LineChart
          width={width}
          height={height}
          data={drawn}
          syncId={syncId}
          margin={CHART_MARGIN}
          {...selectableChartProps(onChartClick)}
        >
          <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border" />
          <XAxis {...xAxisProps(labels, plotWidth)} />
          <YAxis {...Y_AXIS_PROPS} domain={[0, yMax]} ticks={yTicks} tickFormatter={formatTick} />
          <Tooltip
            {...TOOLTIP_PROPS}
            // Recharts drops a line with no value from what it hands the card,
            // and draws no card at all for a bucket in which every line is a
            // gap. A chart with words for "not recorded" has something to say
            // there, so it keeps them; one without draws no card, as before.
            filterNull={notRecordedLabel === undefined}
            cursor={{
              className: "stroke-muted-foreground",
              strokeDasharray: "3 3",
            }}
            content={
              <MultiSeriesLineTooltip
                series={series}
                formatValue={formatValue}
                formatSeriesDetail={formatSeriesDetail}
                zeroIsMeasured={zeroIsMeasured}
                nothingCountedLabel={nothingCountedLabel}
                chartHeight={height}
                comparisonSeries={comparisonSeries}
                partialLabel={partialLabel}
                notRecordedLabel={notRecordedLabel}
              />
            }
          />
          {selectionMarks(data, { selectedIndex, cursorIndex }, "point")}
          {comparisonLines(comparisonSeries)}
          {series.map((entry) => (
            <Line
              key={entry.id}
              // A function reads the value out of `values`, and a null stays a
              // null: with connectNulls off, that is recharts' word for a gap.
              dataKey={(datum: DrawnLineDatum) => (datum.closed ?? datum.values)[entry.id] ?? null}
              name={entry.name}
              type="linear"
              stroke={entry.color}
              strokeWidth={2}
              connectNulls={false}
              dot={isolatedPointDot}
              // Recharts draws no active dot on a point whose value is null,
              // so hovering a gap highlights the lines that have a value there
              // and quietly skips the ones that do not.
              activeDot={{ r: 3 }}
              isAnimationActive={false}
            />
          ))}
          {hasPartial
            ? partialStretchLines(series, (datum: DrawnLineDatum, id) => datum.stretch?.[id] ?? null)
            : null}
          {markerLine(data, marker, "point")}
        </LineChart>
      )}
    </StackedChartShell>
  );
}

/**
 * A datum as the chart draws it: the caller's own, and beside it, when the
 * chart has a partial bucket, each series' value for the closed line and for
 * the stretch reaching the partial bucket (see `partialLineValues`).
 */
type DrawnLineDatum = MultiSeriesLineDatum & {
  closed?: Record<string, number | null>;
  stretch?: Record<string, number | null>;
};

type MultiSeriesLineTooltipProps = {
  series: StackedSeries[];
  formatValue: (value: number) => string;
  formatSeriesDetail?: (seriesId: string, datum: MultiSeriesLineDatum) => string | null;
  zeroIsMeasured: boolean;
  nothingCountedLabel: string;
  /** The chart's height, handed on so the card can fit itself to it. */
  chartHeight: number;
  comparisonSeries?: StackedSeries[];
  partialLabel?: string;
  notRecordedLabel?: string;
  /** Injected by recharts when it clones this element. */
  active?: boolean;
  payload?: { payload?: MultiSeriesLineDatum }[];
};

/**
 * The hover card for one bucket: the drawn series that counted something
 * there, in the caller's order (see `tooltipRows` for what "counted" means and
 * why a gap and a zero are left out). When none did, the card keeps its
 * bucket label and says so in one muted line: that nothing was counted, or,
 * where every series is a gap and the chart has the words, that nothing was
 * recorded. A partial bucket says so first, and the comparison series' rows
 * follow the series' own.
 */
export function MultiSeriesLineTooltip({
  series,
  formatValue,
  formatSeriesDetail,
  zeroIsMeasured,
  nothingCountedLabel,
  chartHeight,
  comparisonSeries,
  partialLabel,
  notRecordedLabel,
  active,
  payload,
}: MultiSeriesLineTooltipProps) {
  const datum = payload?.[0]?.payload;
  if (!active || !datum) {
    return null;
  }
  const reading = tooltipRows(
    series,
    datum.values,
    formatValue,
    (seriesId) => formatSeriesDetail?.(seriesId, datum) ?? null,
    { zeroIsMeasured },
  );
  const comparisonRows = comparisonTooltipRows(comparisonSeries, datum, formatValue);
  return (
    <ChartTooltipCard
      label={datum.label}
      chartHeight={chartHeight}
      rows={[...reading.rows, ...comparisonRows]}
      notes={tooltipNotes(datum, { partialLabel })}
      // Said only when the comparison has nothing to show either, or the card
      // would say "nothing" above a row.
      emptyLabel={
        comparisonRows.length === 0
          ? tooltipEmptyLabel(reading, { nothingCountedLabel, notRecordedLabel })
          : undefined
      }
    />
  );
}
