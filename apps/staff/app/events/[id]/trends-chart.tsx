"use client";

import { useMemo } from "react";

import { StackedBarChart, type StackedBarSeries } from "@ticket-pos/ui";

import {
  trendsSeries,
  trendsYMax,
  type TrendsDay,
  type TrendsMeasure,
} from "@/lib/sales-trends";

type TrendsChartProps = {
  /** What this chart is called on screen, e.g. "Tickets sold". */
  title: string;
  /** One line saying what the figure means, read before the bars are. */
  description: string;
  /** The whole zero-filled span. The chart filters it; it is never refetched. */
  days: TrendsDay[];
  /** The drawn Ticket Types with their colours, in catalog display order. */
  series: StackedBarSeries[];
  /**
   * A short paragraph beneath the bars, for a figure that needs defending
   * against a reasonable misreading. Absent on a chart that speaks for itself.
   */
  note?: string;
  /** Which figure to plot: tickets sold, or Takings. */
  measure: TrendsMeasure;
  /** Renders a figure exactly, for the tooltip. */
  formatValue: (value: number) => string;
  /** Renders a figure short, for an axis tick. Defaults to `formatValue`. */
  formatTickValue?: (value: number) => string;
  totalLabel: string;
  /** Shared with the other chart so hovering one highlights the same day in the
   * other, and the pair is read as one surface rather than two. */
  syncId?: string;
};

/**
 * One measure of the Sales Trends surface, drawn as a stacked bar per day.
 *
 * The measure is a parameter rather than the component's identity because the
 * surface is a pair: the same days, the same Ticket Types and the same chip
 * selection plotted twice, once counting tickets and once counting Takings.
 * Everything that differs between the two — the field, the formatter, the words
 * — arrives as a prop, so the second chart is a second instance rather than a
 * second component.
 *
 * The selection itself lives above this component for the same reason: one chip
 * row must drive both charts, and state held here could only drive one.
 */
export function TrendsChart({
  title,
  description,
  note,
  days,
  series,
  measure,
  formatValue,
  formatTickValue,
  totalLabel,
  syncId,
}: TrendsChartProps) {
  const selected = useMemo(() => series.map((entry) => entry.id), [series]);
  // Recomputed on every chip click and on nothing else: the matrix is already
  // in hand, so filtering and rescaling never touch the network.
  const data = useMemo(() => trendsSeries(days, selected, measure), [days, selected, measure]);
  const yMax = useMemo(() => trendsYMax(data), [data]);

  return (
    <section className="space-y-1">
      <h3 className="text-sm font-medium">{title}</h3>
      <p className="text-xs text-muted-foreground">{description}</p>
      <StackedBarChart
        className="pt-2"
        data={data}
        series={series}
        yMax={yMax}
        formatValue={formatValue}
        formatTickValue={formatTickValue}
        totalLabel={totalLabel}
        syncId={syncId}
        ariaLabel={`${title}, one bar per day, stacked by Ticket Type`}
      />
      {note ? <p className="text-xs text-muted-foreground">{note}</p> : null}
    </section>
  );
}
