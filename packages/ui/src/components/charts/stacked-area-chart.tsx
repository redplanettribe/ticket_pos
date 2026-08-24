"use client";

import * as React from "react";
import { Area, AreaChart, CartesianGrid, Tooltip, XAxis, YAxis } from "recharts";

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

export type StackedAreaChartProps = StackedChartProps;

/**
 * StackedAreaChart draws the same buckets `StackedBarChart` does, as bands
 * stacked one on another rather than as separate bars.
 *
 * The shape carries the meaning. Bars are for figures that stand alone — each
 * one a day's own takings, comparable with its neighbours and independent of
 * them. An area is for a figure that carries its own history: a running total,
 * where the interesting fact is the slope between two points rather than either
 * point's height. Drawing a running total as bars would make every bar nearly as
 * tall as the last and hide the only thing worth seeing.
 *
 * Its geometry, its axis and its hover card are `chart-frame`'s, shared with the
 * bar chart, so a surface offering both shapes moves nothing but the shape when
 * the reader switches: the same axis width, the same tick placement, the same
 * bucket at the same x, the same hover card.
 */
export function StackedAreaChart({
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
}: StackedAreaChartProps) {
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
        <AreaChart width={width} height={height} data={data} syncId={syncId} margin={CHART_MARGIN}>
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
              <StackedChartTooltip
                series={series}
                formatValue={formatValue}
                totalLabel={totalLabel}
                chartHeight={height}
              />
            }
          />
          {series.map((entry) => (
            <Area
              key={entry.id}
              dataKey={(datum: StackedDatum) => datum.values[entry.id] ?? 0}
              name={entry.name}
              stackId="stack"
              // Straight segments between buckets, never a smoothed curve. A
              // spline drawn through a running total bulges between two days and
              // reads as sales the Event did not make on a day it made none —
              // and on a monotone series it can dip below a point it just passed,
              // which would draw the total going backwards. The only honest
              // statement about the gap between two days is a straight line.
              type="linear"
              stroke={entry.color}
              fill={entry.color}
              // Bands are translucent so a band's own thickness stays legible
              // where it sits on top of another; the stroke keeps its upper edge
              // crisp, which is the line the reader is actually following.
              fillOpacity={0.35}
              strokeWidth={2}
              // The dot per bucket is off for the same reason the bar chart caps
              // its bars: a year of days would draw several hundred markers over
              // each other. Hovering still names the exact bucket.
              dot={false}
              activeDot={{ r: 3 }}
              isAnimationActive={false}
            />
          ))}
        </AreaChart>
      )}
    </StackedChartShell>
  );
}
