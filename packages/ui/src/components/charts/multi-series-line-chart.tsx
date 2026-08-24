"use client";

import * as React from "react";
import { CartesianGrid, Line, LineChart, Tooltip, XAxis, YAxis } from "recharts";

import {
  CHART_MARGIN,
  ChartTooltipCard,
  StackedChartShell,
  TOOLTIP_PROPS,
  Y_AXIS_PROPS,
  xAxisProps,
  type StackedSeries,
} from "./chart-frame";

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
export type MultiSeriesLineDatum = {
  key: string;
  label: string;
  values: Record<string, number | null>;
};

/** What a multi-series line chart is told. See `StackedChartProps` for why the
 * axis geometry (`yMax`, `yTicks`, `plotWidth`) is owned by the caller. */
export type MultiSeriesLineChartProps = {
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
  syncId,
  height = 320,
  ariaLabel,
  className,
}: MultiSeriesLineChartProps) {
  const formatTick = formatTickValue ?? formatValue;
  const labels = data.map((datum) => datum.label);
  return (
    <StackedChartShell
      plotWidth={plotWidth}
      height={height}
      yMax={yMax}
      yTicks={yTicks}
      formatTick={formatTick}
      ariaLabel={ariaLabel}
      className={className}
    >
      {(width) => (
        <LineChart width={width} height={height} data={data} syncId={syncId} margin={CHART_MARGIN}>
          <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border" />
          <XAxis {...xAxisProps(labels, plotWidth)} />
          <YAxis {...Y_AXIS_PROPS} domain={[0, yMax]} ticks={yTicks} tickFormatter={formatTick} />
          <Tooltip
            {...TOOLTIP_PROPS}
            cursor={{
              className: "stroke-muted-foreground",
              strokeDasharray: "3 3",
            }}
            content={
              <MultiSeriesLineTooltip
                series={series}
                formatValue={formatValue}
                formatSeriesDetail={formatSeriesDetail}
                chartHeight={height}
              />
            }
          />
          {series.map((entry) => (
            <Line
              key={entry.id}
              // A function reads the value out of `values`, and a null stays a
              // null: with connectNulls off, that is recharts' word for a gap.
              dataKey={(datum: MultiSeriesLineDatum) => datum.values[entry.id] ?? null}
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
        </LineChart>
      )}
    </StackedChartShell>
  );
}

/** What recharts hands a custom dot: where the point sits, which index it is,
 * and the whole series' points beside it. Spelled structurally rather than
 * imported, so a recharts type rename cannot break the chart. */
type LinePointDotProps = {
  cx?: number;
  cy?: number;
  index?: number;
  stroke?: string;
  points?: readonly { value?: unknown }[];
};

/**
 * hasValue reads a point the way the line does: null is a gap, and recharts
 * carries the gap through as a null (or absent) value on the point.
 */
function hasValue(point: { value?: unknown } | undefined): boolean {
  return point !== undefined && point.value !== null && point.value !== undefined;
}

/**
 * The dot drawn on a point with a gap on both sides — and on no other point.
 *
 * A line is drawn between consecutive values, so a value with a gap before it
 * and a gap after it has no segment to be part of; with dots off it is
 * invisible, and a Daily rate that landed a sale on one day out of seven
 * would draw nothing at all while the tooltip insisted there was a point. The
 * dot is the mark that point gets instead of a segment. Points inside a run
 * keep no dot: the line already states them, and a dot on every point turns a
 * trajectory into a scatter.
 */
function isolatedPointDot({ cx, cy, index, stroke, points }: LinePointDotProps) {
  if (
    typeof cx !== "number" ||
    typeof cy !== "number" ||
    !Number.isFinite(cx) ||
    !Number.isFinite(cy) ||
    index === undefined ||
    points === undefined
  ) {
    return null;
  }
  if (!hasValue(points[index]) || hasValue(points[index - 1]) || hasValue(points[index + 1])) {
    return null;
  }
  return <circle cx={cx} cy={cy} r={3} fill={stroke} stroke="none" />;
}

type MultiSeriesLineTooltipProps = {
  series: StackedSeries[];
  formatValue: (value: number) => string;
  formatSeriesDetail?: (seriesId: string, datum: MultiSeriesLineDatum) => string | null;
  /** The chart's height, handed on so the card can fit itself to it. */
  chartHeight: number;
  /** Injected by recharts when it clones this element. */
  active?: boolean;
  payload?: { payload?: MultiSeriesLineDatum }[];
};

/**
 * The hover card for one bucket: every drawn series in the caller's order. A
 * null value is stated as an em dash — the gap in the line, said out loud —
 * rather than formatted as the zero it is not, and carries no detail line: the
 * detail states a division, and there was none.
 */
export function MultiSeriesLineTooltip({
  series,
  formatValue,
  formatSeriesDetail,
  chartHeight,
  active,
  payload,
}: MultiSeriesLineTooltipProps) {
  const datum = payload?.[0]?.payload;
  if (!active || !datum) {
    return null;
  }
  return (
    <ChartTooltipCard
      label={datum.label}
      chartHeight={chartHeight}
      rows={series.map((entry) => {
        const value = datum.values[entry.id] ?? null;
        return {
          id: entry.id,
          name: entry.name,
          color: entry.color,
          value: value === null ? "—" : formatValue(value),
          detail: value === null ? null : (formatSeriesDetail?.(entry.id, datum) ?? null),
        };
      })}
    />
  );
}
