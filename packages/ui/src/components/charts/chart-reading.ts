/**
 * What a chart of a trend reading can say beyond its series: a comparison
 * series beside them, a bucket that is still running, the bucket recording
 * began at, the bucket a reader chose, and a figure that was never recorded.
 *
 * Every one of these is opt-in. A chart handed none of them draws exactly what
 * it drew before they existed.
 *
 * A plain module beside `chart-geometry.ts` for the same reason: the rules
 * must be testable under `node --test`, which cannot parse JSX, and nothing in
 * them depends on React. The charts and `chart-frame` draw what this decides.
 */

/**
 * What any bucket may carry besides its values, whatever shape it is drawn in.
 *
 * `partial` marks a bucket that is still running (today): it is drawn visibly
 * apart from a closed one and its hover card says so.
 *
 * `comparison` holds the value of each comparison series for this bucket,
 * keyed by that series' id. It is aligned by construction: the value sits in
 * the bucket it is compared with. An absent series, an absent `comparison` and
 * a null all mean the comparison has nothing here, which is drawn as a gap and
 * never as a zero.
 */
export type ChartBucketReading = {
  partial?: boolean;
  comparison?: Readonly<Record<string, number | null | undefined>>;
};

/**
 * The value of one comparison series in one bucket, or null for a gap. A zero
 * stays a zero: the span it comes from was counted and came to nothing.
 */
export function comparisonValue(bucket: ChartBucketReading, comparisonId: string): number | null {
  return bucket.comparison?.[comparisonId] ?? null;
}

/**
 * One series of a line chart, split into the line through its closed buckets
 * and the stretch that reaches a partial one.
 *
 * A line cannot change its stroke halfway, so a partial bucket is drawn by a
 * second line: it holds the partial bucket's value and the value of the closed
 * bucket before it, so the two lines meet there, and nothing anywhere else.
 * The closed line gives up the partial bucket, and with it the segment leading
 * to it. When the bucket before is a gap, nothing leads to the partial one and
 * it stands as a point of its own.
 */
export function partialLineValues(
  values: readonly (number | null)[],
  partial: readonly boolean[],
): { closed: (number | null)[]; partial: (number | null)[] } {
  const closed: (number | null)[] = [];
  const stretch: (number | null)[] = [];
  values.forEach((value, index) => {
    const isPartial = partial[index] === true;
    const next = values[index + 1] ?? null;
    const leadsToPartial = partial[index + 1] === true && next !== null;
    closed.push(isPartial ? null : value);
    stretch.push(isPartial || leadsToPartial ? value : null);
  });
  return { closed, partial: stretch };
}

/** Where a marker stands and which side of it its label is written on. */
export type MarkerPlacement = {
  index: number;
  labelSide: "before" | "after";
};

/**
 * Where the marker for `markerKey` stands among the chart's buckets, or null
 * when the chart does not draw that bucket (the span begins after it, or ends
 * before it), in which case no marker is drawn.
 *
 * The label is written after the mark while the mark is in the first half of
 * the chart and before it otherwise, so it is written toward the middle and
 * never off either end of the plot.
 */
export function markerPlacement(keys: readonly string[], markerKey: string): MarkerPlacement | null {
  const index = keys.indexOf(markerKey);
  if (index < 0) {
    return null;
  }
  return { index, labelSide: index <= (keys.length - 1) / 2 ? "after" : "before" };
}

/**
 * The bucket the keyboard stands on when it arrives at a chart: the selected
 * one, or the newest when none is selected, since the newest is what a reader
 * came for. -1 when the chart has no buckets.
 */
export function initialBucketIndex(keys: readonly string[], selectedKey: string | null): number {
  const selected = selectedKey === null ? -1 : keys.indexOf(selectedKey);
  return selected >= 0 ? selected : keys.length - 1;
}

/**
 * The bucket a key press moves the keyboard to, or null when the key moves
 * nothing and is left to the browser. The arrows stop at the ends instead of
 * wrapping: a span has a first and a last day, and wrapping would jump a year.
 */
export function bucketKeyTarget(key: string, index: number, count: number): number | null {
  if (count <= 0) {
    return null;
  }
  switch (key) {
    case "ArrowRight":
      return Math.min(count - 1, index + 1);
    case "ArrowLeft":
      return Math.max(0, index - 1);
    case "Home":
      return 0;
    case "End":
      return count - 1;
    default:
      return null;
  }
}

/** How a chart places a bucket: bars give it a band, a line a point. */
export type BucketShape = "band" | "point";

/**
 * The index of the bucket a click landed on, or null when it landed outside
 * the plot.
 *
 * `offset` is the click's distance from the plot's left edge. The buckets are
 * placed as recharts places them on a category axis inset by `edgePadding` at
 * each end (see `xAxisEdgePadding`): bars share the room between the insets as
 * equal bands, and a line's points are spread from one inset to the other, a
 * single point in the middle. The click belongs to the bucket whose middle is
 * nearest, which is also the bucket recharts' hover card names there, so a
 * reader chooses the bucket they were just shown.
 *
 * Worked out from the click and not read from recharts' own idea of the
 * hovered bucket, which it updates a frame after the pointer moves: a tap, or
 * a click straight after a move, would choose the bucket the pointer was on
 * before.
 */
export function bucketIndexAt(
  offset: number,
  frame: { count: number; plotWidth: number; edgePadding: number; shape: BucketShape },
): number | null {
  const { count, plotWidth, edgePadding, shape } = frame;
  if (count <= 0 || !Number.isFinite(offset) || offset < 0 || offset > plotWidth) {
    return null;
  }
  const inner = plotWidth - 2 * edgePadding;
  if (count === 1 || inner <= 0) {
    return count === 1 ? 0 : null;
  }
  const along = offset - edgePadding;
  const index =
    shape === "band"
      ? Math.floor(along / (inner / count))
      : // Halfway between two points goes to the earlier one, as recharts has it.
        Math.ceil(along / (inner / (count - 1)) - 0.5);
  return Math.min(count - 1, Math.max(0, index));
}

/**
 * Whether a chart has buckets and series but not one value among them: the
 * figure was not recorded, which is not the same reading as a chart of zeros
 * and must not be drawn as one.
 *
 * Only the drawn series are asked. A chart with no buckets or no series is
 * empty for its own reasons and is left to draw as it always has.
 */
export function nothingRecorded(
  data: readonly { values: Readonly<Record<string, number | null | undefined>> }[],
  seriesIds: readonly string[],
): boolean {
  if (data.length === 0 || seriesIds.length === 0) {
    return false;
  }
  return data.every((datum) => seriesIds.every((id) => (datum.values[id] ?? null) === null));
}
