"use client";

import * as React from "react";
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from "recharts";

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
 */
export type MultiSeriesDatum = {
  key: string;
  label: string;
  values: Record<string, number>;
};

/** What a multi-series bar chart is told. See `StackedChartProps` for why the
 * axis geometry (`yMax`, `yTicks`, `plotWidth`) is owned by the caller. */
export type MultiSeriesBarChartProps = {
  data: MultiSeriesDatum[];
  series: StackedSeries[];
  yMax: number;
  yTicks?: number[];
  plotWidth: number;
  formatValue: (value: number) => string;
  formatTickValue?: (value: number) => string;
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
  syncId,
  height = 320,
  ariaLabel,
  className,
}: MultiSeriesBarChartProps) {
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
        <BarChart width={width} height={height} data={data} syncId={syncId} margin={CHART_MARGIN}>
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
            cursor={{ className: "fill-muted", opacity: 0.4 }}
            content={<MultiSeriesChartTooltip series={series} formatValue={formatValue} />}
          />
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
            />
          ))}
        </BarChart>
      )}
    </StackedChartShell>
  );
}

type MultiSeriesChartTooltipProps = {
  series: StackedSeries[];
  formatValue: (value: number) => string;
  /** Injected by recharts when it clones this element. */
  active?: boolean;
  payload?: { payload?: MultiSeriesDatum }[];
};

/**
 * The hover card for one bucket: every drawn series with its exact figure, in
 * the caller's series order, zeros stated rather than omitted — and no total
 * row, for the reason `MultiSeriesDatum` has no total.
 */
export function MultiSeriesChartTooltip({
  series,
  formatValue,
  active,
  payload,
}: MultiSeriesChartTooltipProps) {
  const datum = payload?.[0]?.payload;
  if (!active || !datum) {
    return null;
  }
  return (
    <div className="rounded-md border bg-popover px-3 py-2 text-popover-foreground shadow-md">
      <p className="mb-1 text-xs font-medium">{datum.label}</p>
      <ul className="space-y-0.5">
        {series.map((entry) => (
          <li key={entry.id} className="flex items-center gap-2 text-xs">
            <span
              aria-hidden
              className="size-2 shrink-0 rounded-[2px]"
              style={{ backgroundColor: entry.color }}
            />
            <span className="text-muted-foreground">{entry.name}</span>
            <span className="ml-auto tabular-nums">{formatValue(datum.values[entry.id] ?? 0)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
