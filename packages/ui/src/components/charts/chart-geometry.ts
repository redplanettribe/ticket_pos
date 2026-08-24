/**
 * The arithmetic behind every chart on a surface: where the axes sit, how the
 * X ticks are thinned and inset, how a hover card lays itself out and where it
 * may stand.
 *
 * It is a plain module rather than part of `chart-frame.tsx` because it must
 * be testable without a DOM — the ui package's tests run under `node --test`,
 * which strips types but cannot parse JSX — and because none of it depends on
 * React. `chart-frame` re-exports the constants under the names surfaces
 * already import.
 */

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
 * How many buckets to skip between X tick labels, so labels stay about
 * `X_TICK_MIN_GAP` apart whatever the bucket count is.
 *
 * `edgePadding` is the inset each end of the axis gives up to its first and
 * last label (see `xAxisEdgePadding`); the buckets share what is left, and the
 * interval is worked out from that or the labels would sit closer than asked.
 * Recharts counts an interval of 0 as "label every bucket".
 */
export function xTickInterval(bucketCount: number, plotWidth: number, edgePadding = 0): number {
  const axisWidth = plotWidth - 2 * edgePadding;
  if (bucketCount <= 0 || axisWidth <= 0) {
    return 0;
  }
  const bucketWidth = axisWidth / bucketCount;
  return Math.max(0, Math.ceil(X_TICK_MIN_GAP / bucketWidth) - 1);
}

/**
 * Which buckets the X axis labels: every `xTickInterval`-th from the first,
 * and always the last.
 *
 * Counting from the first alone, the last bucket is labelled only when the
 * count happens to divide — a week of hours, labelled every eighth, ends at
 * bucket 160 of 167 and the current hour goes unnamed. That is the bucket the
 * live ranges scroll the reader to, so it is the one label the axis cannot
 * drop. The last-but-one regular label is dropped instead when it stands
 * closer to the last than the interval allows, since two labels that close
 * overprint each other; the first label and the even spacing of the rest are
 * kept as they were.
 *
 * Handed to recharts as explicit `ticks` rather than as `interval:
 * "preserveEnd"`, which would space the labels from the end instead and
 * measure every label to do it — the per-render cost `xTickInterval` exists
 * to avoid.
 */
export function xTickLabels(labels: readonly string[], plotWidth: number, edgePadding = 0): string[] {
  const last = labels.length - 1;
  if (last < 0) {
    return [];
  }
  const step = xTickInterval(labels.length, plotWidth, edgePadding) + 1;
  const ticks: string[] = [];
  for (let index = 0; index < last; index += step) {
    ticks.push(labels[index]!);
  }
  const lastRegular = (ticks.length - 1) * step;
  if (ticks.length > 1 && last - lastRegular < step) {
    ticks.pop();
  }
  ticks.push(labels[last]!);
  return ticks;
}

/**
 * How wide a character of an axis label is taken to be, in pixels, at
 * `AXIS_TICK.fontSize`. Inter measures about 5.7 at 12px; the estimate is
 * rounded up so a label of digits and wide letters still fits, because the cost
 * of guessing high is a few pixels of inset and the cost of guessing low is a
 * label with its first character under the axis.
 */
const AXIS_LABEL_CHAR_WIDTH = 6.5;

/** The most an axis end is ever inset, so a freak label cannot eat the plot. */
const X_AXIS_EDGE_PADDING_MAX = 48;

/** The least an axis end is inset: enough that a point drawn on the very edge
 * does not have its stroke halved by the plot's boundary. */
const X_AXIS_EDGE_PADDING_MIN = 8;

/**
 * The distance between each end of the plot and the first and last bucket, so
 * their labels are drawn whole.
 *
 * A category axis puts the first bucket's centre at the plot's left edge (for
 * a line or an area) or half a bucket in (for bars), and centres the label on
 * it — so half the label falls left of the plot, under the opaque strip the Y
 * axis is pinned in, and the last label's other half falls off the end of the
 * drawing. "24 ago" was read as "ago" on one side and "24 ag" on the other.
 *
 * Sized from the labels actually drawn rather than fixed, because an hourly
 * label ("24 ago, 14:00") is twice the width of a daily one and a fixed inset
 * wide enough for the first would waste a tenth of a narrow plot on the second.
 * Half the widest label, because the label is centred on its bucket.
 */
export function xAxisEdgePadding(labels: readonly string[]): number {
  const widest = labels.reduce((max, label) => Math.max(max, label.length), 0);
  const half = Math.ceil((widest * AXIS_LABEL_CHAR_WIDTH) / 2);
  return Math.min(X_AXIS_EDGE_PADDING_MAX, Math.max(X_AXIS_EDGE_PADDING_MIN, half));
}

/**
 * The X axis props every chart on a surface shares, worked out from the labels
 * it draws and the width it draws them in: the inset that keeps the end labels
 * whole, and the labels that keep the rest apart. Spelled once so two shapes
 * of the same buckets cannot inset them differently and slide a bucket between
 * views.
 *
 * The labels are stated as `ticks` and the interval set to zero, which tells
 * recharts to draw exactly those and thin nothing itself: the thinning is
 * `xTickLabels`' job, because it alone knows to keep the last bucket. Ticks
 * only ever affect the drawn labels — recharts finds the hovered bucket from
 * the whole category domain, so an unlabelled hour still has its hover card.
 */
export function xAxisProps(labels: readonly string[], plotWidth: number) {
  const padding = xAxisEdgePadding(labels);
  return {
    dataKey: "label",
    tickLine: false,
    axisLine: false,
    height: X_AXIS_HEIGHT,
    interval: 0,
    ticks: xTickLabels(labels, plotWidth, padding),
    padding: { left: padding, right: padding },
    tick: AXIS_TICK,
  } as const;
}

/**
 * The attribute `ChartScrollArea` marks itself with, so a hover card drawn
 * inside it can find the window it must stay in without being handed a ref
 * through recharts — which clones the card and passes only what it knows
 * about.
 */
export const CHART_SCROLL_AREA_ATTRIBUTE = "data-chart-scroll-area";

/**
 * The heights a hover card is laid out with, in pixels. Estimates of what the
 * card's classes render to — a `text-xs` line is 16px, a row gap 2px — rather
 * than measurements, because the card decides its columns before it is drawn.
 * They err high on purpose: a card laid out for slightly less room than it has
 * is a card with a little air in it; one laid out for more is a card taller
 * than its chart, and a chart inside a scrolling element grows a scrollbar to
 * show the part it cannot.
 */
export const TOOLTIP_ROW_HEIGHT = 18;
/** A row with the muted detail line under it: two lines and the gap. */
export const TOOLTIP_DETAIL_ROW_HEIGHT = 34;
/** The card's own padding and border, and the bucket label with its margin. */
export const TOOLTIP_CHROME_HEIGHT = 40;
/** The total row a stacked chart's card ends with: rule, padding and a line. */
export const TOOLTIP_FOOTER_HEIGHT = 26;

/**
 * How a hover card's rows are split into columns so the card fits the chart it
 * hovers over.
 *
 * A surface with sixteen Affiliate Links has seventeen rows, and seventeen rows
 * of text are taller than a 320px chart. Recharts keeps the card inside the
 * plot area only when it can; a card taller than the plot is pinned to the top
 * and hangs out of the bottom, and since the chart sits inside a horizontally
 * scrolling element — where `overflow-x: auto` forces `overflow-y: auto` too —
 * the overhang becomes a vertical scrollbar over the chart and a card the
 * reader cannot see the end of. So the rows are dealt into as few columns as
 * fit the room, and balanced across them: two columns of nine read better than
 * one of fourteen beside one of three.
 *
 * `rowHeight` is the tallest kind of row the card holds, applied to every row,
 * because the columns are a CSS grid and a grid row is as tall as its tallest
 * cell — a card whose rows differ in height is laid out as if they were all the
 * tallest, so that is the honest number to plan with.
 */
export function tooltipColumnLayout(
  rowCount: number,
  rowHeight: number,
  availableHeight: number,
): { columns: number; rowsPerColumn: number } {
  if (rowCount <= 0) {
    return { columns: 1, rowsPerColumn: 1 };
  }
  const rowsThatFit = Math.max(1, Math.floor(availableHeight / rowHeight));
  const columns = Math.ceil(rowCount / rowsThatFit);
  return { columns, rowsPerColumn: Math.ceil(rowCount / columns) };
}

/**
 * The gap recharts leaves between the hover cursor and the card it places
 * beside it. Stated here, and handed to recharts as its `offset`, because the
 * card mirrors itself across the cursor by the same gap (see
 * `scrollWindowShift`) and the two must agree or the mirror lands on the
 * cursor.
 */
export const TOOLTIP_CURSOR_GAP = 10;

/**
 * How far a box placed inside a scrolling window must be moved to be seen
 * whole, along one axis: negative to move it back, positive to move it on,
 * zero when it already fits.
 *
 * Recharts places a hover card so it stays inside the chart, but a chart drawn
 * wider than its card (a week of hours is thousands of pixels) is mostly out of
 * view, and "inside the chart" is no promise of "inside the window". Hovering
 * a bucket near the window's right edge put the card off the edge, where only
 * scrolling would find it.
 *
 * A box past the window's end was placed `cursorGap` after the cursor, so its
 * first resort is the mirror position, the same gap before the cursor — what
 * recharts itself does at the chart's end, and what keeps the hovered bucket
 * out from under the card. Only when there is no room on that side is the box
 * pulled straight back to the window's end. A box too wide for the window is
 * pinned to the window's start — nothing fits it, and the start is where
 * reading begins.
 */
export function scrollWindowShift(
  box: { start: number; size: number },
  window: { start: number; end: number },
  cursorGap?: number,
): number {
  if (box.size >= window.end - window.start) {
    return window.start - box.start;
  }
  const overflow = box.start + box.size - window.end;
  if (overflow > 0) {
    if (cursorGap !== undefined) {
      const mirrored = box.start - 2 * cursorGap - box.size;
      if (mirrored >= window.start) {
        return mirrored - box.start;
      }
    }
    return -overflow;
  }
  if (box.start < window.start) {
    return window.start - box.start;
  }
  return 0;
}
