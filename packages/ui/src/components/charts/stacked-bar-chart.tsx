"use client";

import * as React from "react";
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from "recharts";

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
  /**
   * The width in pixels of the plotting area alone — the part that holds the
   * bars, not the Y axis.
   *
   * The chart is drawn at exactly this width rather than stretched to its
   * container, because a bucket must keep a minimum width however many buckets
   * there are: squeezing a year of days into a card makes every bar a sliver,
   * and widening the bucket to fix that would mean a bar stood for a different
   * span at different ranges. So the caller multiplies its bucket count by the
   * width a bucket is owed, and the chart overflows if that is more than there
   * is room for.
   *
   * A chart drawn this way must be placed inside a horizontally scrolling
   * element (`ChartScrollArea`): the Y axis pins itself to that element's left
   * edge, so without one there is nothing for it to pin to.
   */
  plotWidth: number;
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
 *
 * It is now load-bearing twice over: it is also the width of the strip the axis
 * is pinned in while the plot scrolls, so a measured axis would leave the strip
 * either short of the axis or over the bars.
 */
const Y_AXIS_WIDTH = 64;

/**
 * The space around the plot area, shared by the drawn chart and the pinned copy
 * of its Y axis so the two cannot drift apart vertically.
 *
 * `left` is zero on purpose: the Y axis is the only thing between the plot area
 * and the chart's left edge, so the pinned copy is exactly `Y_AXIS_WIDTH` wide
 * and covers exactly what it should. A left margin would have to be added to
 * that width in two places and would eventually be added to one.
 */
const CHART_MARGIN = { top: 8, right: 8, bottom: 8, left: 0 } as const;

/**
 * The height the X axis always takes. Recharts defaults to this; stating it
 * makes the drawn chart and the pinned axis agree by construction rather than
 * by both happening to inherit the same default.
 */
const X_AXIS_HEIGHT = 30;

const AXIS_TICK = { fontSize: 12 } as const;

/**
 * The roughest spacing an X tick label is allowed, in pixels.
 *
 * Ticks are thinned by a plain numeric interval rather than by recharts'
 * `preserveStartEnd`, which measures every label to find collisions — a
 * per-render DOM measurement per bucket, which is what turns a chart of several
 * hundred days from slow-to-draw into slow-to-scroll.
 */
const X_TICK_MIN_GAP = 72;

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

/**
 * The Y axis, spelled once and rendered twice: in the chart, where it
 * establishes the scale, and in the pinned copy the reader actually reads.
 *
 * Shared rather than duplicated because the two must be the same axis — a tick
 * that moved in one and not the other would label the bars with a lie.
 */
const Y_AXIS_PROPS = {
  allowDecimals: false,
  tickLine: false,
  axisLine: false,
  width: Y_AXIS_WIDTH,
  tick: AXIS_TICK,
} as const;

/**
 * How many buckets to skip between X tick labels, so labels stay about
 * `X_TICK_MIN_GAP` apart whatever the bucket count is.
 *
 * Recharts counts an interval of 0 as "label every bucket".
 */
function xTickInterval(bucketCount: number, plotWidth: number): number {
  if (bucketCount <= 0 || plotWidth <= 0) {
    return 0;
  }
  const bucketWidth = plotWidth / bucketCount;
  return Math.max(0, Math.ceil(X_TICK_MIN_GAP / bucketWidth) - 1);
}

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
 */
export function StackedBarChart({
  data,
  series,
  yMax,
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
  // The Y axis sits to the left of the plot area and the right margin to the
  // right of it, so the drawing is wider than the plot the caller asked for.
  // Adding them here keeps `plotWidth` an honest statement about the buckets.
  const width = Y_AXIS_WIDTH + plotWidth + CHART_MARGIN.right;
  return (
    <div className={cn("relative flex w-max", className)} style={{ height }}>
      {/* The axis the reader reads. It is a second drawing of the same axis
          rather than a relocation of the first, because recharts owns where a
          tick lands and the only way to be sure two axes agree about that is to
          let it decide both. It is opaque so the bars pass behind it, and
          `aria-hidden` because the chart beside it already carries the accessible
          name — a screen reader has no use for a second, mute copy. */}
      <div
        aria-hidden
        className="sticky left-0 z-10 shrink-0 overflow-hidden bg-card"
        style={{ width: Y_AXIS_WIDTH, height, marginRight: -Y_AXIS_WIDTH }}
      >
        <PinnedYAxis yMax={yMax} formatTick={formatTick} height={height} />
      </div>
      <div role="img" aria-label={ariaLabel} className="shrink-0" style={{ width, height }}>
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
          <YAxis
            // Pinned to the caller's yMax so the axis answers to the current
            // selection rather than to the whole catalog.
            {...Y_AXIS_PROPS}
            domain={[0, yMax]}
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
              maxBarSize={MAX_BAR_SIZE}
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      </div>
    </div>
  );
}

/**
 * The pinned Y axis: the same axis as the chart's own, drawn again in a strip
 * that stays put while the plot scrolls under it.
 *
 * It is a whole recharts chart rather than a handful of positioned labels
 * because tick placement is recharts' arithmetic — where the ticks for
 * `[0, yMax]` fall, and where the plot area starts once the margins and the X
 * axis have taken their space. Reimplementing that here would work until a
 * version bump moved a tick by a pixel and quietly mislabelled every bar.
 *
 * Everything that decides the vertical geometry is shared with the chart
 * (`CHART_MARGIN`, `X_AXIS_HEIGHT`, `Y_AXIS_PROPS`, `height`) and nothing that
 * decides it depends on width, so the two agree whatever the span. It carries a
 * single datum and no bars: the X axis must be present to take up its space,
 * but it has nothing to say and its ticks are off.
 */
function PinnedYAxis({
  yMax,
  formatTick,
  height,
}: {
  yMax: number;
  formatTick: (value: number) => string;
  height: number;
}) {
  // Wide enough that the plot area is a positive width — recharts needs
  // somewhere to put a chart — and clipped back to the axis by the strip above.
  const width = Y_AXIS_WIDTH + 8 + CHART_MARGIN.right;
  return (
    <BarChart
      width={width}
      height={height}
      data={PINNED_AXIS_DATA}
      margin={CHART_MARGIN}
      // Off, because recharts otherwise makes its canvas focusable and keyboard
      // navigable — and a focus stop hidden from assistive technology is a trap
      // rather than a courtesy. The chart beside this one keeps its own.
      accessibilityLayer={false}
    >
      <XAxis
        dataKey="label"
        tick={false}
        tickLine={false}
        axisLine={false}
        height={X_AXIS_HEIGHT}
      />
      <YAxis {...Y_AXIS_PROPS} domain={[0, yMax]} tickFormatter={formatTick} />
    </BarChart>
  );
}

/** One nameless bucket, so the pinned axis has a chart to be an axis of. */
const PINNED_AXIS_DATA = [{ label: "" }];

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
