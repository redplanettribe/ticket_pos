"use client";

import * as React from "react";
import { Bar, BarChart, XAxis, YAxis } from "recharts";

import { cn } from "../../lib/utils";
import {
  AXIS_TICK,
  CHART_MARGIN,
  CHART_SCROLL_AREA_ATTRIBUTE,
  TOOLTIP_CHROME_HEIGHT,
  TOOLTIP_CURSOR_GAP,
  TOOLTIP_DETAIL_ROW_HEIGHT,
  TOOLTIP_FOOTER_HEIGHT,
  TOOLTIP_ROW_HEIGHT,
  X_AXIS_HEIGHT,
  Y_AXIS_WIDTH,
  scrollWindowShift,
  tooltipColumnLayout,
} from "./chart-geometry";

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
 * The geometry lives in `chart-geometry.ts`, where a test can reach it without
 * a DOM; it is re-exported here under the names every surface imports.
 */
export {
  AXIS_TICK,
  CHART_MARGIN,
  CHART_PLOT_INSET,
  CHART_SCROLL_AREA_ATTRIBUTE,
  X_AXIS_HEIGHT,
  Y_AXIS_WIDTH,
  xAxisEdgePadding,
  xAxisProps,
  xTickInterval,
  xTickLabels,
} from "./chart-geometry";

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

/**
 * One row of a hover card: a series' swatch and name, its figure as the caller
 * spells it, and — for the views that state the division or the money behind
 * a figure — a muted line under it.
 */
export type ChartTooltipRow = {
  id: string;
  name: string;
  color: string;
  value: string;
  detail?: string | null;
};

/**
 * The hover card every chart on a surface draws: the bucket's label, a row per
 * series in the caller's order, and whatever footer the chart's shape calls
 * for. Shared so switching view changes what the numbers count and never how
 * they are read.
 *
 * It does two things a plain list would not, both because it lives inside a
 * horizontally scrolling element. It deals its rows into columns sized to the
 * chart's height, so a surface with many series cannot make a card taller than
 * its chart — an overhang there becomes a vertical scrollbar over the plot
 * (see `tooltipColumnLayout`). And once recharts has placed it, it checks
 * where it landed against the part of the scroll area a reader can see and
 * moves itself back inside (see `scrollWindowShift`): recharts keeps it in the
 * chart, and the chart is mostly off screen.
 */
export function ChartTooltipCard({
  label,
  rows,
  chartHeight,
  footer,
}: {
  label: string;
  rows: ChartTooltipRow[];
  /** The chart's height, which is all the room the card may take. */
  chartHeight: number;
  footer?: React.ReactNode;
}) {
  const hasDetail = rows.some((row) => row.detail != null);
  const available =
    chartHeight -
    2 * CHART_MARGIN.top -
    TOOLTIP_CHROME_HEIGHT -
    (footer === undefined ? 0 : TOOLTIP_FOOTER_HEIGHT);
  const { rowsPerColumn } = tooltipColumnLayout(
    rows.length,
    hasDetail ? TOOLTIP_DETAIL_ROW_HEIGHT : TOOLTIP_ROW_HEIGHT,
    available,
  );
  const { ref, shift } = useKeptInScrollWindow();
  return (
    <div
      ref={ref}
      className="rounded-md border bg-popover px-3 py-2 text-popover-foreground shadow-md"
      style={{ transform: `translate(${shift.x}px, ${shift.y}px)` }}
    >
      <p className="mb-1 text-xs font-medium">{label}</p>
      <ul
        className="grid grid-flow-col gap-x-4 gap-y-0.5"
        style={{ gridTemplateRows: `repeat(${rowsPerColumn}, auto)` }}
      >
        {rows.map((row) => (
          <li key={row.id} className="text-xs">
            <div className="flex items-center gap-2">
              <span
                aria-hidden
                className="size-2 shrink-0 rounded-[2px]"
                style={{ backgroundColor: row.color }}
              />
              <span className="text-muted-foreground">{row.name}</span>
              <span className="ml-auto pl-2 tabular-nums">{row.value}</span>
            </div>
            {row.detail == null ? null : (
              <p className="pl-4 text-[11px] text-muted-foreground tabular-nums">{row.detail}</p>
            )}
          </li>
        ))}
      </ul>
      {footer}
    </div>
  );
}

/**
 * Where a card must move to be inside the visible part of the scroll area it
 * was drawn in, measured after each placement.
 *
 * Measured rather than computed, because the card does not know the scroll
 * offset and recharts does not either: it positions the card in the chart's
 * own coordinates, and where the chart is relative to the window is the scroll
 * area's business. A layout effect reads both boxes after the placement and
 * before paint, so the card is never seen in the wrong place first. The window
 * it keeps to starts after the pinned Y axis, which sits over the plot and
 * would otherwise sit over the card.
 *
 * The position is read off the card's frame — the wrapper recharts positions,
 * which is exactly where the card sits before any shift — and never off the
 * shifted card itself. Reading the card and subtracting the shift back out
 * looks equivalent and is not: Chrome reports a transformed box in single
 * precision, so `left - shift` misses the frame's position by a few
 * hundred-thousandths, the next shift differs from the last by that much, and
 * the effect re-renders itself until React gives up (error #185) — a crash on
 * hover at any sub-pixel geometry, which a high-DPI screen makes routine.
 * Measured from the frame, the answer is the same on every pass, so the effect
 * settles after one. Only the size is the card's own: a transform does not
 * change it. Recharts must not animate the frame (see `TOOLTIP_PROPS`): a box
 * mid-transition measures as wherever it happens to be that frame.
 */
function useKeptInScrollWindow() {
  const ref = React.useRef<HTMLDivElement>(null);
  const [shift, setShift] = React.useState({ x: 0, y: 0 });
  React.useLayoutEffect(() => {
    const card = ref.current;
    const frame = card?.parentElement;
    const area = card?.closest<HTMLElement>(`[${CHART_SCROLL_AREA_ATTRIBUTE}]`);
    if (!card || !frame || !area) {
      return;
    }
    const placed = frame.getBoundingClientRect();
    const size = card.getBoundingClientRect();
    const seen = area.getBoundingClientRect();
    const next = {
      x: scrollWindowShift(
        { start: placed.left, size: size.width },
        { start: seen.left + Y_AXIS_WIDTH, end: seen.left + area.clientWidth },
        TOOLTIP_CURSOR_GAP,
      ),
      y: scrollWindowShift(
        { start: placed.top, size: size.height },
        { start: seen.top, end: seen.top + area.clientHeight },
      ),
    };
    if (next.x !== shift.x || next.y !== shift.y) {
      setShift(next);
    }
  });
  return { ref, shift };
}

/**
 * What a chart's `Tooltip` is told beyond its content, spelled once. No
 * animation, because the card measures where it landed (see
 * `useKeptInScrollWindow`) and a card sliding into place measures as wherever
 * it is mid-slide. A z-index above the pinned axis strip's, so a card wider
 * than the window — pinned to its start, over the axis — is drawn over the
 * axis rather than under it. The offset is the gap the card mirrors itself by.
 */
export const TOOLTIP_PROPS = {
  isAnimationActive: false,
  offset: TOOLTIP_CURSOR_GAP,
  wrapperStyle: { zIndex: 20 },
} as const;

type StackedChartTooltipProps = {
  series: StackedSeries[];
  formatValue: (value: number) => string;
  totalLabel: string;
  /** The chart's height, handed on so the card can fit itself to it. */
  chartHeight: number;
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
 */
export function StackedChartTooltip({
  series,
  formatValue,
  totalLabel,
  chartHeight,
  active,
  payload,
}: StackedChartTooltipProps) {
  const datum = payload?.[0]?.payload;
  if (!active || !datum) {
    return null;
  }
  return (
    <ChartTooltipCard
      label={datum.label}
      chartHeight={chartHeight}
      rows={series.map((entry) => ({
        id: entry.id,
        name: entry.name,
        color: entry.color,
        value: formatValue(datum.values[entry.id] ?? 0),
      }))}
      footer={
        <p className="mt-1 flex items-center gap-4 border-t pt-1 text-xs font-medium">
          <span>{totalLabel}</span>
          <span className="ml-auto tabular-nums">{formatValue(datum.total)}</span>
        </p>
      }
    />
  );
}
