"use client";

import { useMemo } from "react";

import { StackedBarChart, type StackedBarSeries } from "@ticket-pos/ui";

import {
  trendsSeries,
  trendsYMax,
  trendsYTicks,
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
   * How wide to draw the plot area. Handed down rather than worked out here so
   * that every chart on the surface is drawn at one width: two charts that each
   * sized themselves would agree today and disagree the first time one of them
   * was given a day the other did not have.
   */
  plotWidth: number;
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
 *
 * It renders inside the surface's shared scroll area, which is why its heading
 * pins itself to the left: a heading that scrolled away with the bars would
 * leave a reader four months into a long span looking at an unlabelled chart.
 */
export function TrendsChart({
  title,
  description,
  days,
  series,
  plotWidth,
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
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);

  return (
    <section className="w-max">
      <div className="sticky left-0 w-max space-y-1 bg-card pb-2 pr-4">
        <h3 className="text-sm font-medium">{title}</h3>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <StackedBarChart
        data={data}
        series={series}
        yMax={yMax}
        yTicks={yTicks}
        plotWidth={plotWidth}
        formatValue={formatValue}
        formatTickValue={formatTickValue}
        totalLabel={totalLabel}
        syncId={syncId}
        ariaLabel={`${title}, one bar per day, stacked by Ticket Type`}
      />
    </section>
  );
}
