/**
 * What a trend tile works out before it draws: which of its four states a
 * change is, and where its small trend line runs.
 *
 * A plain module, like `chart-geometry.ts`, so the rules are testable under
 * `node --test` without a DOM.
 */

/**
 * The four states a tile's change can be in. `nothing` is "nothing to compare
 * against": the comparison span had no data or came to zero, so there is no
 * ratio to state. It is not `unchanged`, which is a comparison that was made
 * and found no difference.
 */
export type TrendDirection = "growth" | "decline" | "unchanged" | "nothing";

/**
 * How finely a tile states a change: one decimal of a percent, which is a
 * thousandth of the ratio.
 */
const CHANGE_STEPS_PER_UNIT = 1000;

/**
 * A change as the tile states it: the signed ratio (0.25 is a quarter more)
 * rounded to one decimal of a percent, or null when there is no change to
 * state (an absent one, or one that is not a finite number).
 *
 * This is the one place the rounding lives. The tile reads the direction from
 * this number (`trendDirection`) and hands this number to the caller to
 * spell, so the arrow and the percentage beside it cannot disagree: a ratio
 * of 0.0004 is 0 here, drawn as no change and printed as 0%.
 *
 * A half rounds away from zero on both sides, so a growth and a decline of
 * the same size are stated the same size. The result is never a negative
 * zero.
 */
export function roundedTrendChange(change: number | null | undefined): number | null {
  if (change === null || change === undefined || !Number.isFinite(change)) {
    return null;
  }
  const steps = Math.round(Math.abs(change) * CHANGE_STEPS_PER_UNIT);
  if (steps === 0) {
    return 0;
  }
  return (Math.sign(change) * steps) / CHANGE_STEPS_PER_UNIT;
}

/**
 * The state of a change stated as a signed ratio (0.25 is a quarter more).
 * An absent change is `nothing`, and so is one that is not a finite number: a
 * division by an empty comparison is never drawn as growth without end.
 *
 * The direction is that of the change as the tile prints it
 * (`roundedTrendChange`), not of the raw ratio: a change too small to print
 * is `unchanged`.
 */
export function trendDirection(change: number | null | undefined): TrendDirection {
  const stated = roundedTrendChange(change);
  if (stated === null) {
    return "nothing";
  }
  if (stated > 0) {
    return "growth";
  }
  return stated < 0 ? "decline" : "unchanged";
}

export type SparklinePoint = { x: number; y: number };

/**
 * The small trend line of a tile, as coordinates in a box of the given size:
 * `runs` are the stretches of consecutive values, each drawn as one polyline,
 * and `points` the values with a gap on both sides, which no segment can reach
 * and which are drawn as dots.
 *
 * Buckets are spread evenly across the box less `inset` at each edge, a single
 * bucket in the middle. A bucket with no value keeps its place and breaks the
 * line, for the reason the line chart breaks there: missing is not zero. The
 * scale runs from zero to the largest value, never from the smallest, so a
 * steady figure is drawn steady at the top and a small wobble is not stretched
 * into a cliff.
 */
export function sparklineShape(
  values: readonly (number | null)[],
  box: { width: number; height: number; inset: number },
): { runs: SparklinePoint[][]; points: SparklinePoint[] } {
  const top = box.inset;
  const bottom = box.height - box.inset;
  const left = box.inset;
  const right = box.width - box.inset;
  const max = values.reduce<number>((largest, value) => Math.max(largest, value ?? 0), 0);
  const step = values.length > 1 ? (right - left) / (values.length - 1) : 0;
  const start = values.length > 1 ? left : (left + right) / 2;

  const runs: SparklinePoint[][] = [];
  const points: SparklinePoint[] = [];
  let run: SparklinePoint[] = [];
  const close = () => {
    if (run.length > 1) {
      runs.push(run);
    } else if (run.length === 1) {
      points.push(run[0]!);
    }
    run = [];
  };
  values.forEach((value, index) => {
    if (value === null) {
      close();
      return;
    }
    const share = max > 0 ? Math.max(0, value) / max : 0;
    run.push({ x: start + index * step, y: bottom - share * (bottom - top) });
  });
  close();
  return { runs, points };
}
