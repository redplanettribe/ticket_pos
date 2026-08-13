/**
 * The categorical palette shared by the staff app's charts.
 *
 * Colours are looked up by an index the caller owns rather than assigned in
 * arrival order, because the whole point is stability: a Ticket Type keeps its
 * colour every time a chart is opened, so staff learn the chart instead of
 * re-reading the legend. Sales Trends passes a Ticket Type's `sort_order`,
 * which is the catalog's own display order and does not move when sales do.
 *
 * The palette is deliberately fixed here rather than read from CSS variables:
 * a stable colour must survive a theme's accent being retuned, and the chart
 * needs the literal value to hand to an SVG fill.
 */
const CHART_PALETTE = [
  "#2563eb", // blue
  "#f59e0b", // amber
  "#10b981", // emerald
  "#ec4899", // pink
  "#8b5cf6", // violet
  "#14b8a6", // teal
  "#ef4444", // red
  "#84cc16", // lime
  "#0ea5e9", // sky
  "#a855f7", // purple
  "#f97316", // orange
  "#64748b", // slate
] as const;

/**
 * chartSeriesColor returns the palette entry for a series index, wrapping round
 * when a catalog is longer than the palette. Negative and non-integer indexes
 * are tolerated rather than thrown on — a chart must still draw when the data
 * is odd, and a repeated colour is a cosmetic loss, not a broken surface.
 */
export function chartSeriesColor(index: number): string {
  if (!Number.isFinite(index)) {
    return CHART_PALETTE[0];
  }
  const slot = Math.trunc(index) % CHART_PALETTE.length;
  return CHART_PALETTE[slot < 0 ? slot + CHART_PALETTE.length : slot];
}

/** How many distinct colours the palette holds before it repeats. */
export const CHART_PALETTE_SIZE = CHART_PALETTE.length;
