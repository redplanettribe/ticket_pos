"use client";

import * as React from "react";
import { CartesianGrid, Line, LineChart, Tooltip, XAxis, YAxis } from "recharts";

import {
  AXIS_TICK,
  CHART_MARGIN,
  StackedChartShell,
  X_AXIS_HEIGHT,
  Y_AXIS_PROPS,
  xTickInterval,
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
          <XAxis
            dataKey="label"
            tickLine={false}
            axisLine={false}
            height={X_AXIS_HEIGHT}
            interval={xTickInterval(data.length, plotWidth)}
            tick={AXIS_TICK}
          />
          <YAxis {...Y_AXIS_PROPS} domain={[0, yMax]} ticks={yTicks} tickFormatter={formatTick} />
          <Tooltip
            cursor={{
              className: "stroke-muted-foreground",
              strokeDasharray: "3 3",
            }}
            content={
              <MultiSeriesLineTooltip
                series={series}
                formatValue={formatValue}
                formatSeriesDetail={formatSeriesDetail}
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
  /** Injected by recharts when it clones this element. */
  active?: boolean;
  payload?: { payload?: MultiSeriesLineDatum }[];
};

/**
 * The hover card for one bucket: every drawn series in the caller's order. A
 * null value is stated as an em dash — the gap in the line, said out loud —
 * rather than formatted as the zero it is not.
 */
export function MultiSeriesLineTooltip({
  series,
  formatValue,
  formatSeriesDetail,
  active,
  payload,
}: MultiSeriesLineTooltipProps) {
  const datum = payload?.[0]?.payload;
  if (!active || !datum) {
    return null;
  }
  return (
    <div className="rounded-md border bg-popover px-3 py-2 text-popover-foreground shadow-md">
      <p className="mb-1 text-xs font-medium">{datum.label}</p>
      <ul className="space-y-0.5">
        {series.map((entry) => {
          const value = datum.values[entry.id] ?? null;
          const detail = value === null ? null : (formatSeriesDetail?.(entry.id, datum) ?? null);
          return (
            <li key={entry.id} className="text-xs">
              <div className="flex items-center gap-2">
                <span
                  aria-hidden
                  className="size-2 shrink-0 rounded-[2px]"
                  style={{ backgroundColor: entry.color }}
                />
                <span className="text-muted-foreground">{entry.name}</span>
                <span className="ml-auto tabular-nums">
                  {value === null ? "—" : formatValue(value)}
                </span>
              </div>
              {detail === null ? null : (
                <p className="pl-4 text-[11px] text-muted-foreground tabular-nums">{detail}</p>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
