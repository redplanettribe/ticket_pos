"use client";

import * as React from "react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { cn } from "../../lib/utils";

/**
 * One stacking series: a category that contributes a segment to every bar.
 *
 * `id` keys the value out of a datum, `name` is what a reader is shown, and
 * `color` is decided by the caller rather than by arrival order so a category
 * keeps its colour between loads (see `chartSeriesColor`).
 */
export type StackedBarSeries = {
  id: string;
  name: string;
  color: string;
};

/**
 * One bar: a bucket, its axis label, the value each series contributed, and the
 * height of the whole stack.
 *
 * `values` carries only what the chart should draw — a caller filtering
 * categories out hands over a datum without them, so filtering is genuinely a
 * filter and not a hidden segment. `total` is the caller's own sum for the
 * tooltip, kept beside the values so the two can never disagree about rounding.
 */
export type StackedBarDatum = {
  key: string;
  label: string;
  values: Record<string, number>;
  total: number;
};

export type StackedBarChartProps = {
  data: StackedBarDatum[];
  series: StackedBarSeries[];
  /**
   * The top of the Y axis. Owned by the caller because rescaling to a filtered
   * selection is a decision about the data, not about the drawing: leaving it to
   * the library would make "the axis rescales when you deselect" an accident of
   * whatever recharts inferred.
   */
  yMax: number;
  /** Renders a value for the tooltip, where a reader wants it exact. */
  formatValue: (value: number) => string;
  /**
   * Renders a value for an axis tick, where a reader wants it short. Defaults to
   * `formatValue`; money charts pass an abbreviated form so a tick fits the
   * fixed axis width (see `Y_AXIS_WIDTH`).
   */
  formatTickValue?: (value: number) => string;
  /** Heading for the tooltip's total row, e.g. "Total tickets". */
  totalLabel: string;
  /**
   * Ties this chart's hover cursor to another's. Two charts sharing a value
   * highlight the same bucket together, so a stacked pair reads as one surface.
   */
  syncId?: string;
  height?: number;
  /** Accessible name — an SVG chart is otherwise mute to a screen reader. */
  ariaLabel: string;
  className?: string;
};

/**
 * The width the Y axis always takes, whatever it is labelled with.
 *
 * Deliberately a constant rather than a prop, and deliberately not left to
 * recharts to measure. Two charts stacked one above the other only line up if
 * their plot areas start at the same x, and a measured axis width is decided by
 * the widest tick label — so a chart of tickets ("8") and a chart of money
 * ("$1.3K") would size their axes differently and slide the same day to two
 * different horizontal positions. Alignment is the whole point of a stacked
 * pair, so the width that guarantees it is not something a caller can vary.
 *
 * Callers whose labels would not fit shorten the label (`formatTickValue`)
 * rather than widening the axis.
 */
const Y_AXIS_WIDTH = 64;

/**
 * StackedBarChart draws one bar per bucket, segmented by series in the order
 * `series` is given.
 *
 * It is deliberately measure-agnostic: it knows nothing of tickets, money or
 * Ticket Types, only of buckets, categories and a formatter. Sales Trends plots
 * tickets through one instance and Takings through a second, and the two differ
 * by their data and their formatters alone.
 */
export function StackedBarChart({
  data,
  series,
  yMax,
  formatValue,
  formatTickValue,
  totalLabel,
  syncId,
  height = 320,
  ariaLabel,
  className,
}: StackedBarChartProps) {
  const formatTick = formatTickValue ?? formatValue;
  return (
    <div className={cn("w-full", className)} role="img" aria-label={ariaLabel} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={data} syncId={syncId} margin={{ top: 8, right: 8, bottom: 8, left: 8 }}>
          <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border" />
          <XAxis
            dataKey="label"
            tickLine={false}
            axisLine={false}
            interval="preserveStartEnd"
            minTickGap={16}
            tick={{ fontSize: 12 }}
          />
          <YAxis
            // Pinned to the caller's yMax so the axis answers to the current
            // selection rather than to the whole catalog.
            domain={[0, yMax]}
            allowDecimals={false}
            tickLine={false}
            axisLine={false}
            width={Y_AXIS_WIDTH}
            tick={{ fontSize: 12 }}
            tickFormatter={formatTick}
          />
          <Tooltip
            cursor={{ className: "fill-muted", opacity: 0.4 }}
            content={
              <StackedBarTooltip
                series={series}
                formatValue={formatValue}
                totalLabel={totalLabel}
              />
            }
          />
          {series.map((entry) => (
            <Bar
              key={entry.id}
              // A function reads the value out of `values` so a series id can be
              // any string (a Ticket Type's UUID) without colliding with the
              // datum's own fields.
              dataKey={(datum: StackedBarDatum) => datum.values[entry.id] ?? 0}
              name={entry.name}
              stackId="stack"
              fill={entry.color}
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

type StackedBarTooltipProps = {
  series: StackedBarSeries[];
  formatValue: (value: number) => string;
  totalLabel: string;
  /** Injected by recharts when it clones this element. */
  active?: boolean;
  payload?: { payload?: StackedBarDatum }[];
};

/**
 * The hover card for one bar: every drawn series with its exact figure, then
 * the stack's total.
 *
 * It reads the datum off the payload rather than the payload's own entries so
 * the rows stay in the caller's series order and a series that contributed
 * nothing to this bucket is still stated as zero — the reader asked about the
 * day, and "GA sold none" is an answer.
 */
function StackedBarTooltip({
  series,
  formatValue,
  totalLabel,
  active,
  payload,
}: StackedBarTooltipProps) {
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
      <p className="mt-1 flex items-center gap-4 border-t pt-1 text-xs font-medium">
        <span>{totalLabel}</span>
        <span className="ml-auto tabular-nums">{formatValue(datum.total)}</span>
      </p>
    </div>
  );
}
