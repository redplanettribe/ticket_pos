"use client";

import { useMemo } from "react";

import type { AppLocale } from "@ticket-pos/locale";
import { StackedAreaChart, StackedBarChart, type StackedSeries } from "@ticket-pos/ui";

import {
  cumulativeTrends,
  trendsSeries,
  trendsYMax,
  trendsYTicks,
  type TrendsDay,
  type TrendsMeasure,
  type TrendsView,
} from "@/lib/sales-trends";

type TrendsChartProps = {
  /** What this chart is called on screen, e.g. "Tickets sold". */
  title: string;
  /** One line saying what the figure means, read before the bars are. */
  description: string;
  /** The whole zero-filled span. The chart filters it; it is never refetched. */
  days: TrendsDay[];
  /** The drawn Ticket Types with their colours, in catalog display order. */
  series: StackedSeries[];
  /**
   * Which counting to draw, and therefore which shape: the Daily view's own
   * figure per day as bars, or the running total up to each day as an area.
   *
   * Handed down rather than held here for the same reason the chip selection is:
   * one control moves both charts on the surface, and a view held in a chart
   * could only move that chart.
   */
  view: TrendsView;
  /**
   * How wide to draw the plot area. Handed down rather than worked out here so
   * that every chart on the surface is drawn at one width: two charts that each
   * sized themselves would agree today and disagree the first time one of them
   * was given a day the other did not have.
   */
  plotWidth: number;
  /** Which figure to plot: tickets sold, or Takings. */
  measure: TrendsMeasure;
  /**
   * The reader's Staff Locale, which reaches exactly one thing here: the marks
   * and the month name on the X axis labels. Which DAY each bar is remains the
   * API's answer, already resolved into the Event's own timezone.
   */
  locale: AppLocale;
  /** Renders a figure exactly, for the tooltip. */
  formatValue: (value: number) => string;
  /** Renders a figure short, for an axis tick. Defaults to `formatValue`. */
  formatTickValue?: (value: number) => string;
  totalLabel: string;
  /**
   * What assistive technology is told this chart is. It arrives already worded
   * rather than being assembled from `title` here: gluing a translated title to
   * a translated tail is exactly the concatenation that puts English word order
   * into a Spanish sentence, so the whole sentence is one catalog message the
   * surface fills in.
   */
  ariaLabel: string;
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
  view,
  locale,
  formatValue,
  formatTickValue,
  totalLabel,
  ariaLabel,
  syncId,
}: TrendsChartProps) {
  const selected = useMemo(() => series.map((entry) => entry.id), [series]);
  // Recomputed on every chip click, on every view switch, and on nothing else:
  // the matrix is already in hand, so filtering, accumulating and rescaling
  // never touch the network.
  //
  // The Cumulative view is built by adding up the Daily view rather than by
  // reading the matrix a second way, so the two can never disagree about which
  // days exist or which Ticket Types are in them — and a deselected Ticket Type
  // is absent from the running total rather than hidden inside it.
  const daily = useMemo(
    () => trendsSeries(days, selected, measure, locale),
    [days, selected, measure, locale],
  );
  const data = useMemo(
    () => (view === "cumulative" ? cumulativeTrends(daily) : daily),
    [daily, view],
  );
  // Scaled to what is drawn, so the Cumulative view's axis tops the span's final
  // total rather than its busiest day — otherwise most of the curve would sit
  // above the top of the chart.
  const yMax = useMemo(() => trendsYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);

  // Bars for a figure that stands alone, an area for one carrying its own
  // history: a running total drawn as bars is a row of near-equal columns that
  // hides the only thing it has to say, which is its slope.
  const Chart = view === "cumulative" ? StackedAreaChart : StackedBarChart;

  return (
    <section className="w-max">
      <div className="sticky left-0 w-max space-y-1 bg-card pb-2 pr-4">
        <h3 className="text-sm font-medium">{title}</h3>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <Chart
        data={data}
        series={series}
        yMax={yMax}
        yTicks={yTicks}
        plotWidth={plotWidth}
        formatValue={formatValue}
        formatTickValue={formatTickValue}
        totalLabel={totalLabel}
        syncId={syncId}
        ariaLabel={ariaLabel}
      />
    </section>
  );
}
