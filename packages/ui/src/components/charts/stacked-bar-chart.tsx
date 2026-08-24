"use client";

import * as React from "react";
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from "recharts";

import {
  CHART_MARGIN,
  StackedChartShell,
  StackedChartTooltip,
  TOOLTIP_PROPS,
  Y_AXIS_PROPS,
  xAxisProps,
  type StackedChartProps,
  type StackedDatum,
} from "./chart-frame";

/**
 * The widest a single bar is drawn, however much room its bucket has.
 *
 * A short span keeps a readable plot area (its caller floors the width), and
 * without a cap the few bars in it would inflate to fill that room — a fortnight
 * of sales drawn as five fat slabs, which reads as a different chart from the
 * same Event three months later. The bucket gets the space; the bar does not
 * take all of it.
 */
const MAX_BAR_SIZE = 48;

export type StackedBarChartProps = StackedChartProps;

/**
 * StackedBarChart draws one bar per bucket, segmented by series in the order
 * `series` is given.
 *
 * It is deliberately measure-agnostic: it knows nothing of tickets, money or
 * Ticket Types, only of buckets, categories and a formatter. Sales Trends plots
 * tickets through one instance and Takings through a second, and the two differ
 * by their data and their formatters alone.
 *
 * It draws at a width its caller decides (`plotWidth`) rather than at whatever
 * width it is given, and pins its Y axis over the left edge of the scrolling
 * element it sits in, so a span too long to fit is read by scrolling rather than
 * by squinting. Two such charts placed in one scroll area therefore scroll
 * together and stay aligned without either knowing about the other.
 *
 * Its geometry and its hover card come from `chart-frame`, shared with
 * `StackedAreaChart`: the same buckets drawn in another shape must line up with
 * these bars and read the same way, which they can only do by construction.
 */
export function StackedBarChart({
  data,
  series,
  yMax,
  yTicks,
  plotWidth,
  formatValue,
  formatTickValue,
  totalLabel,
  syncId,
  height = 320,
  ariaLabel,
  className,
}: StackedBarChartProps) {
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
        <BarChart width={width} height={height} data={data} syncId={syncId} margin={CHART_MARGIN}>
          <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border" />
          <XAxis {...xAxisProps(labels, plotWidth)} />
          <YAxis
            // Pinned to the caller's yMax so the axis answers to the current
            // selection rather than to the whole catalog.
            {...Y_AXIS_PROPS}
            domain={[0, yMax]}
            ticks={yTicks}
            tickFormatter={formatTick}
          />
          <Tooltip
            {...TOOLTIP_PROPS}
            cursor={{ className: "fill-muted", opacity: 0.4 }}
            content={
              <StackedChartTooltip
                series={series}
                formatValue={formatValue}
                totalLabel={totalLabel}
                chartHeight={height}
              />
            }
          />
          {series.map((entry) => (
            <Bar
              key={entry.id}
              // A function reads the value out of `values` so a series id can be
              // any string (a Ticket Type's UUID) without colliding with the
              // datum's own fields.
              dataKey={(datum: StackedDatum) => datum.values[entry.id] ?? 0}
              name={entry.name}
              stackId="stack"
              fill={entry.color}
              maxBarSize={MAX_BAR_SIZE}
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      )}
    </StackedChartShell>
  );
}
