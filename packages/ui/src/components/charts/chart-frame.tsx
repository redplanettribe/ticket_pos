"use client";

import * as React from "react";
import { Bar, BarChart, XAxis, YAxis } from "recharts";

import { cn } from "../../lib/utils";

/**
 * The geometry, the axis and the hover card every stacked chart on a surface
 * shares.
 *
 * It exists because a surface can draw the same buckets in more than one shape —
 * Sales Trends draws days as bars in its Daily view and as an area in its
 * Cumulative view — and the two must be the same chart in every respect but the
 * shape. Anything that decides where a tick lands, how wide the axis is, or what
 * a hover says lives here rather than in either chart, so switching view cannot
 * move the furniture.
 */

/**
 * One stacking series: a category that contributes a segment to every bucket.
 *
 * `id` keys the value out of a datum, `name` is what a reader is shown, and
 * `color` is decided by the caller rather than by arrival order so a category
 * keeps its colour between loads (see `chartSeriesColor`).
 */
export type StackedSeries = {
  id: string;
  name: string;
  color: string;
};

/**
 * One bucket: its key, its axis label, the value each series contributed, and
 * the height of the whole stack.
 *
 * `values` carries only what the chart should draw — a caller filtering
 * categories out hands over a datum without them, so filtering is genuinely a
 * filter and not a hidden segment. `total` is the caller's own sum for the
 * tooltip, kept beside the values so the two can never disagree about rounding.
 */
export type StackedDatum = {
  key: string;
  label: string;
  values: Record<string, number>;
  total: number;
};

/** What every stacked chart on a surface is told, whatever shape it draws. */
export type StackedChartProps = {
  data: StackedDatum[];
  series: StackedSeries[];
  /**
   * The top of the Y axis. Owned by the caller because rescaling to a filtered
   * selection is a decision about the data, not about the drawing: leaving it to
   * the library would make "the axis rescales when you deselect" an accident of
   * whatever recharts inferred.
   */
  yMax: number;
  /**
   * Where the Y axis puts its labels. Owned by the caller for the same reason
   * `yMax` is: left to the library, the step is chosen independently of the top
   * of the axis, so the last two labels can end up almost touching and the gaps
   * between them unequal.
   *
   * Used by the drawn axis and by the pinned copy alike — they are one axis
   * shown twice, and a tick in one that is missing from the other would label
   * the buckets with a lie.
   */
  yTicks?: number[];
  /**
   * The width in pixels of the plotting area alone — the part that holds the
   * buckets, not the Y axis.
   *
   * The chart is drawn at exactly this width rather than stretched to its
   * container, because how much room a bucket is owed is a decision only the
   * caller can make: Sales Trends gives a day a minimum width in its Daily view
   * and lets the plot overflow, and fits the whole span inside the card in its
   * Cumulative view. Either way the chart draws what it is told.
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
 * It is now load-bearing three times over: it is also the width of the strip the
 * axis is pinned in while the plot scrolls, and it is what keeps a surface's two
 * shapes — bars and an area — starting at the same x when the view is switched.
 */
export const Y_AXIS_WIDTH = 64;

/**
 * The space around the plot area, shared by every drawn chart and by the pinned
 * copy of its Y axis so they cannot drift apart vertically.
 *
 * `left` is zero on purpose: the Y axis is the only thing between the plot area
 * and the chart's left edge, so the pinned copy is exactly `Y_AXIS_WIDTH` wide
 * and covers exactly what it should. A left margin would have to be added to
 * that width in two places and would eventually be added to one.
 */
export const CHART_MARGIN = { top: 8, right: 8, bottom: 8, left: 0 } as const;

/**
 * The height the X axis always takes. Recharts defaults to this; stating it
 * makes the drawn chart and the pinned axis agree by construction rather than
 * by both happening to inherit the same default.
 */
export const X_AXIS_HEIGHT = 30;

/**
 * How much horizontal room a chart spends on what is not the plot: the Y axis on
 * the left, and the margin on the right.
 *
 * Exported so a caller sizing its plot to the room available can subtract it.
 * Without it the caller would have to guess, and a guess that is too small draws
 * a chart narrower than the space it was given — which reads, correctly, as the
 * chart being shoved to one side of its card.
 */
export const CHART_PLOT_INSET = Y_AXIS_WIDTH + CHART_MARGIN.right;

export const AXIS_TICK = { fontSize: 12 } as const;

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
 * The Y axis, spelled once and rendered wherever an axis is drawn: in the chart,
 * where it establishes the scale, and in the pinned copy the reader reads.
 *
 * Shared rather than duplicated because they must be the same axis — a tick that
 * moved in one and not the other would label the buckets with a lie.
 */
export const Y_AXIS_PROPS = {
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
export function xTickInterval(bucketCount: number, plotWidth: number): number {
  if (bucketCount <= 0 || plotWidth <= 0) {
    return 0;
  }
  const bucketWidth = plotWidth / bucketCount;
  return Math.max(0, Math.ceil(X_TICK_MIN_GAP / bucketWidth) - 1);
}

/**
 * The shell every stacked chart is drawn in: the pinned Y axis on the left, and
 * the plot beside it carrying the accessible name.
 *
 * The recharts root is the caller's, handed the width this works out, because it
 * is the one part that genuinely differs between shapes — a `BarChart` or an
 * `AreaChart`. Everything around it is identical and lives here, so the two
 * shapes cannot drift into different geometry.
 *
 * The extraction deliberately stops at the root. The grid, the axes and the
 * tooltip are recharts' own children and it discovers them by inspecting the
 * element types it was given directly, so a component or fragment wrapping them
 * would be read as an unknown child and silently drawn as nothing. They are
 * therefore spelled out in each chart, from the constants above.
 */
export function StackedChartShell({
  plotWidth,
  height,
  yMax,
  yTicks,
  formatTick,
  ariaLabel,
  className,
  children,
}: {
  plotWidth: number;
  height: number;
  yMax: number;
  yTicks?: number[];
  formatTick: (value: number) => string;
  ariaLabel: string;
  className?: string;
  /** The recharts root, drawn at the width the shell works out for it. */
  children: (width: number) => React.ReactNode;
}) {
  // The Y axis sits to the left of the plot area and the right margin to the
  // right of it, so the drawing is wider than the plot the caller asked for.
  // Adding them here keeps `plotWidth` an honest statement about the buckets.
  const width = Y_AXIS_WIDTH + plotWidth + CHART_MARGIN.right;
  return (
    <div className={cn("relative flex w-max", className)} style={{ height }}>
      <PinnedYAxisStrip yMax={yMax} yTicks={yTicks} formatTick={formatTick} height={height} />
      <div role="img" aria-label={ariaLabel} className="shrink-0" style={{ width, height }}>
        {children(width)}
      </div>
    </div>
  );
}

/**
 * The strip holding the pinned Y axis, and the axis inside it.
 *
 * It is a second drawing of the same axis rather than a relocation of the first,
 * because recharts owns where a tick lands and the only way to be sure two axes
 * agree about that is to let it decide both. It is opaque so the plot passes
 * behind it, and `aria-hidden` because the chart beside it already carries the
 * accessible name — a screen reader has no use for a second, mute copy.
 */
export function PinnedYAxisStrip({
  yMax,
  yTicks,
  formatTick,
  height,
}: {
  yMax: number;
  yTicks?: number[];
  formatTick: (value: number) => string;
  height: number;
}) {
  return (
    <div
      aria-hidden
      className="sticky left-0 z-10 shrink-0 overflow-hidden bg-card"
      style={{ width: Y_AXIS_WIDTH, height, marginRight: -Y_AXIS_WIDTH }}
    >
      <PinnedYAxis yMax={yMax} yTicks={yTicks} formatTick={formatTick} height={height} />
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
 * version bump moved a tick by a pixel and quietly mislabelled every bucket.
 *
 * Everything that decides the vertical geometry is shared with the chart
 * (`CHART_MARGIN`, `X_AXIS_HEIGHT`, `Y_AXIS_PROPS`, `height`) and nothing that
 * decides it depends on width or on the shape being drawn, so a bar chart and an
 * area chart get the same axis in the same place. It carries a single datum and
 * no visible mark: the X axis must be present to take up its space, but it has
 * nothing to say and its ticks are off.
 */
function PinnedYAxis({
  yMax,
  yTicks,
  formatTick,
  height,
}: {
  yMax: number;
  yTicks?: number[];
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
      <YAxis {...Y_AXIS_PROPS} domain={[0, yMax]} ticks={yTicks} tickFormatter={formatTick} />
      {/* An invisible bar, and the reason this axis has any ticks at all.
          Recharts builds a Y axis's scale from the graphical items plotted
          against it; a chart with an axis and nothing to plot renders the axis
          line and no tick labels, however explicit its domain. So the pinned
          copy carries one transparent zero-height bar purely to be something the
          axis is an axis of. Remove it and the strip goes blank — and because it
          is opaque and sits over the real axis, the chart loses its scale
          entirely rather than falling back to the one underneath. */}
      <Bar dataKey="value" fill="none" isAnimationActive={false} />
    </BarChart>
  );
}

/** One nameless, valueless bucket, so the pinned axis has a chart to be an axis of. */
const PINNED_AXIS_DATA = [{ label: "", value: 0 }];

type StackedChartTooltipProps = {
  series: StackedSeries[];
  formatValue: (value: number) => string;
  totalLabel: string;
  /** Injected by recharts when it clones this element. */
  active?: boolean;
  payload?: { payload?: StackedDatum }[];
};

/**
 * The hover card for one bucket: every drawn series with its exact figure, then
 * the stack's total.
 *
 * It reads the datum off the payload rather than the payload's own entries so
 * the rows stay in the caller's series order and a series that contributed
 * nothing to this bucket is still stated as zero — the reader asked about the
 * day, and "GA sold none" is an answer.
 *
 * Shared by every shape on a surface, so switching view changes what the numbers
 * count and never how they are read.
 */
export function StackedChartTooltip({
  series,
  formatValue,
  totalLabel,
  active,
  payload,
}: StackedChartTooltipProps) {
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
