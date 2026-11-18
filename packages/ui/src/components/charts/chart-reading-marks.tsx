"use client";

import * as React from "react";
import { Cell, Line, ReferenceArea, ReferenceLine } from "recharts";

import { CHART_MARGIN, X_AXIS_HEIGHT, Y_AXIS_WIDTH, xAxisEdgePadding } from "./chart-geometry";
import {
  bucketIndexAt,
  bucketKeyTarget,
  comparisonValue,
  initialBucketIndex,
  markerPlacement,
  type BucketShape,
  type ChartBucketReading,
} from "./chart-reading";

/**
 * The marks a chart of a trend reading draws beyond its series, and the
 * keyboard surface a selectable chart is read through. `chart-reading.ts`
 * decides; this draws.
 *
 * The marks are functions returning recharts elements, called by each chart
 * among its own children, and not components wrapping them: a chart's root is
 * handed its lines, bars and reference marks directly, as `StackedChartShell`
 * explains.
 *
 * Nothing here measures the DOM. Every mark is placed by recharts from the
 * bucket it belongs to, and the keyboard surface is a list that needs no
 * coordinates, so there is no layout effect and nothing for one to set (see
 * `useKeptInScrollWindow` in `chart-frame` for the crash a measuring effect
 * once caused on hover).
 *
 * Colours that are not a series' own are the theme's variables, so every mark
 * reads in light and dark alike.
 */

/** One drawn series, as every chart on a surface spells it. Restated here and
 * not imported from `chart-frame`, which imports this module. */
type Series = { id: string; name: string; color: string };

/** A labelled vertical mark at one bucket: where exact recording began. */
export type ChartMarker = {
  /** The key of the bucket the mark stands at. A key the chart does not draw
   * draws no mark. */
  key: string;
  /** What the mark is called, written beside it. The caller's words. */
  label: string;
};

/**
 * Lets a reader choose a bucket, by pointer and by keyboard.
 *
 * The chart reports the choice and draws the selection it is told about; it
 * holds neither. What choosing a bucket twice means (keep it, or let it go) is
 * the caller's to say.
 */
export type ChartBucketSelection = {
  /** The key of the selected bucket, or null when none is. */
  selectedKey: string | null;
  /** Told the key of the bucket a click, Enter or Space chose. */
  onSelect: (key: string) => void;
  /** What assistive technology calls the list of buckets, e.g. "Choose a day
   * to see what it was made of". */
  label: string;
  /** How one bucket is read aloud, and shown beside the plot while the
   * keyboard is on it: its day and its figure, e.g. "12 Jul: 34 tickets". */
  describeBucket: (key: string) => string;
};

/**
 * What the charts that draw a trend reading accept beyond their own props.
 * Every member is optional, and a chart given none draws as it always has.
 */
export type ChartReadingProps = {
  /**
   * Comparison series, each drawn as a faint dashed line over the chart's own
   * series. Their values are read from each bucket's `comparison`, keyed by
   * `id`, and a bucket with none is a gap in the line. The caller's `yMax`
   * must make room for them, as it does for the series.
   */
  comparisonSeries?: Series[];
  /** What the hover card says of a bucket marked `partial`. */
  partialLabel?: string;
  marker?: ChartMarker;
  selection?: ChartBucketSelection;
  /**
   * What is said where nothing was recorded, which is not a zero: in place of
   * the whole chart when no drawn series has a value in any bucket, and in the
   * hover card of a bucket in which none has. Without it such a chart draws an
   * empty plot, as before.
   */
  notRecordedLabel?: string;
};

type Bucket = ChartBucketReading & { key: string; label: string };

/** What recharts hands a custom dot. Spelled structurally and not imported,
 * so a recharts type rename cannot break the chart. */
type LinePointDotProps = {
  cx?: number;
  cy?: number;
  index?: number;
  stroke?: string;
  strokeOpacity?: number | string;
  points?: readonly { value?: unknown }[];
  payload?: ChartBucketReading;
};

/**
 * hasValue reads a point the way the line does: null is a gap, and recharts
 * carries the gap through as a null (or absent) value on the point.
 */
function hasValue(point: { value?: unknown } | undefined): boolean {
  return point !== undefined && point.value !== null && point.value !== undefined;
}

function isPlaced(cx: number | undefined, cy: number | undefined): boolean {
  return typeof cx === "number" && typeof cy === "number" && Number.isFinite(cx) && Number.isFinite(cy);
}

/**
 * The dot drawn on a point with a gap on both sides, and on no other point.
 *
 * A line is drawn between consecutive values, so a value with a gap before it
 * and a gap after it has no segment to be part of; with dots off it is
 * invisible, and a Daily rate that landed a sale on one day out of seven
 * would draw nothing at all while the tooltip insisted there was a point. The
 * dot is the mark that point gets instead of a segment. Points inside a run
 * keep no dot: the line already states them, and a dot on every point turns a
 * trajectory into a scatter.
 */
export function isolatedPointDot({ cx, cy, index, stroke, strokeOpacity, points }: LinePointDotProps) {
  if (!isPlaced(cx, cy) || index === undefined || points === undefined) {
    return null;
  }
  if (!hasValue(points[index]) || hasValue(points[index - 1]) || hasValue(points[index + 1])) {
    return null;
  }
  return <circle key={index} cx={cx} cy={cy} r={3} fill={stroke} fillOpacity={strokeOpacity} stroke="none" />;
}

/**
 * The dot on a partial bucket: a ring, hollow where a closed point is solid,
 * so the bucket still running is told from the closed ones by its shape and
 * not only by the dashes leading to it.
 */
function partialPointDot({ cx, cy, index, stroke, points, payload }: LinePointDotProps) {
  if (!isPlaced(cx, cy) || index === undefined || payload?.partial !== true || !hasValue(points?.[index])) {
    return null;
  }
  return <circle key={index} cx={cx} cy={cy} r={3.5} fill="var(--card)" stroke={stroke} strokeWidth={2} />;
}

/** The dashes of the stretch reaching a partial bucket, and of a partial bar's
 * outline: unfinished, in the convention a dashed outline already has. */
const PARTIAL_DASH = "3 3";

/**
 * The line reaching each series' partial bucket: dashed, ending in a ring.
 * Drawn by the line chart beside the closed lines, which give the partial
 * bucket up (see `partialLineValues`). `stretchOf` reads a series' value for
 * that line out of a datum.
 */
export function partialStretchLines<Datum>(
  series: readonly Series[],
  stretchOf: (datum: Datum, seriesId: string) => number | null,
) {
  return series.map((entry) => (
    <Line
      key={`partial:${entry.id}`}
      dataKey={(datum: Datum) => stretchOf(datum, entry.id)}
      name={entry.name}
      type="linear"
      stroke={entry.color}
      strokeWidth={2}
      strokeDasharray={PARTIAL_DASH}
      connectNulls={false}
      dot={partialPointDot}
      activeDot={{ r: 3 }}
      legendType="none"
      isAnimationActive={false}
    />
  ));
}

/**
 * One cell per bucket of a bar series, or null when no bucket is partial and
 * the bars are left exactly as they were. A partial bucket's bar is drawn
 * faint inside a dashed outline: still the series' colour, visibly not a
 * finished bar.
 */
export function partialBarCells(data: readonly Bucket[], color: string) {
  if (!data.some((datum) => datum.partial === true)) {
    return null;
  }
  return data.map((datum) =>
    datum.partial === true ? (
      <Cell
        key={datum.key}
        fill={color}
        fillOpacity={0.3}
        stroke={color}
        strokeWidth={1}
        strokeDasharray={PARTIAL_DASH}
      />
    ) : (
      <Cell key={datum.key} fill={color} />
    ),
  );
}

/**
 * The comparison series: a thin, dashed, half-transparent line each, over
 * whatever the chart draws, broken where a bucket has no comparison (see
 * `comparisonValue`). Faint by three means and not by colour alone, so it is
 * told from the series it is compared with in either theme and by a reader
 * who cannot tell the hues apart. A value alone between two gaps is a dot, as
 * on any line.
 *
 * In a bar chart recharts places its points at the middle of each band, which
 * is what aligns the comparison bucket to bucket with the bars.
 */
export function comparisonLines(comparisonSeries: readonly Series[] | undefined) {
  return (comparisonSeries ?? []).map((entry) => (
    <Line
      key={`comparison:${entry.id}`}
      dataKey={(datum: ChartBucketReading) => comparisonValue(datum, entry.id)}
      name={entry.name}
      type="linear"
      stroke={entry.color}
      strokeWidth={1.5}
      strokeOpacity={COMPARISON_OPACITY}
      strokeDasharray="5 4"
      connectNulls={false}
      dot={isolatedPointDot}
      activeDot={false}
      legendType="none"
      isAnimationActive={false}
    />
  ));
}

/** How faint a comparison line is drawn, and the swatch of its hover row. */
export const COMPARISON_OPACITY = 0.5;

/**
 * The labelled vertical mark at the marker's bucket, or null when there is no
 * marker or the chart does not draw its bucket. On bars it stands at the start
 * of the band, since recording began with that bucket and not halfway through
 * it; on a line it stands on the point.
 */
export function markerLine(data: readonly Bucket[], marker: ChartMarker | undefined, shape: BucketShape) {
  if (marker === undefined) {
    return null;
  }
  const placement = markerPlacement(
    data.map((datum) => datum.key),
    marker.key,
  );
  if (placement === null) {
    return null;
  }
  return (
    <ReferenceLine
      key="marker"
      x={data[placement.index]!.label}
      position={shape === "band" ? "start" : "middle"}
      stroke="var(--muted-foreground)"
      strokeDasharray="2 3"
      label={{
        value: marker.label,
        // Recharts names the corner of the mark's own box, which has no width:
        // "left" starts the text at the line, so it runs after it.
        position: placement.labelSide === "after" ? "insideTopLeft" : "insideTopRight",
        fontSize: 11,
        fill: "var(--muted-foreground)",
      }}
    />
  );
}

/**
 * The marks for the selected bucket and for the one the keyboard is on.
 *
 * Selected is a solid mark in the text colour (a tinted band behind bars, a
 * vertical rule through a line's point); the keyboard's is a dashed one in the
 * focus colour. Solid against dashed, so the two are told apart without
 * colour, and both from the hover cursor, which is dashed and muted.
 */
export function selectionMarks(
  data: readonly Bucket[],
  { selectedIndex, cursorIndex }: { selectedIndex: number | null; cursorIndex: number | null },
  shape: BucketShape,
) {
  const selected = selectedIndex === null ? undefined : data[selectedIndex];
  const cursor = cursorIndex === null ? undefined : data[cursorIndex];
  if (shape === "band") {
    return [
      selected === undefined ? null : (
        <ReferenceArea
          key="selected"
          x1={selected.label}
          x2={selected.label}
          fill="var(--foreground)"
          fillOpacity={0.08}
          stroke="var(--foreground)"
          strokeOpacity={0.6}
        />
      ),
      cursor === undefined ? null : (
        <ReferenceArea
          key="cursor"
          x1={cursor.label}
          x2={cursor.label}
          fill="none"
          stroke="var(--ring)"
          strokeWidth={2}
          strokeDasharray="4 3"
        />
      ),
    ];
  }
  return [
    selected === undefined ? null : (
      <ReferenceLine key="selected" x={selected.label} stroke="var(--foreground)" strokeWidth={1.5} />
    ),
    cursor === undefined ? null : (
      <ReferenceLine key="cursor" x={cursor.label} stroke="var(--ring)" strokeWidth={2} strokeDasharray="4 3" />
    ),
  ];
}

/** What a chart's recharts root calls on a click: its own state, which is not
 * read (see `bucketIndexAt` for why), and the click. */
export type ChartClickHandler = (state: unknown, event: React.MouseEvent<Element>) => void;

/**
 * Everything a chart needs to let its buckets be chosen: the click handler
 * for its recharts root, which bucket is selected and which the keyboard is
 * on (for `selectionMarks`), and the keyboard surface to hand its shell.
 *
 * Without a `selection` it returns nothing to draw and nothing to handle.
 *
 * The keyboard surface is a listbox laid over the plot: one tab stop, one
 * option per bucket. The arrow keys, Home and End move between buckets, Enter
 * and Space choose the one the keyboard is on, and assistive technology reads
 * each bucket as the caller describes it, with whether it is selected. The
 * options are read, not seen, except the one the keyboard is on, which is
 * shown as a chip at the plot's corner while the list has focus: a
 * sighted reader moving by keyboard sees the figure they are on, which the
 * hover card would have told a pointer. The list lets every pointer event
 * through to the chart beneath it, which is where a click is read from and
 * placed by `bucketIndexAt`.
 *
 * It sits beside the plot and not inside it, because the plot is an image to
 * assistive technology and what is inside an image is not read.
 */
export function useBucketSelection(
  data: readonly Bucket[],
  selection: ChartBucketSelection | undefined,
  frame: { plotWidth: number; height: number; shape: BucketShape },
): {
  overlay: React.ReactNode;
  selectedIndex: number | null;
  cursorIndex: number | null;
  onChartClick: ChartClickHandler | undefined;
} {
  const id = React.useId();
  // Where the keyboard was last moved or a pointer last chose, as a bucket key
  // so it survives the span being redrawn with other buckets around it.
  const [cursorKey, setCursorKey] = React.useState<string | null>(null);
  const [focused, setFocused] = React.useState(false);
  if (selection === undefined) {
    return { overlay: null, selectedIndex: null, cursorIndex: null, onChartClick: undefined };
  }

  const keys = data.map((datum) => datum.key);
  const selectedAt = selection.selectedKey === null ? -1 : keys.indexOf(selection.selectedKey);
  const movedTo = cursorKey === null ? -1 : keys.indexOf(cursorKey);
  const activeIndex = movedTo >= 0 ? movedTo : initialBucketIndex(keys, selection.selectedKey);
  const optionId = (index: number) => `${id}-bucket-${index}`;

  const choose = (key: string) => {
    setCursorKey(key);
    selection.onSelect(key);
  };

  const overlay =
    keys.length === 0 ? null : (
      <div
        role="listbox"
        tabIndex={0}
        aria-label={selection.label}
        aria-orientation="horizontal"
        aria-activedescendant={optionId(activeIndex)}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            choose(keys[activeIndex]!);
            return;
          }
          const target = bucketKeyTarget(event.key, activeIndex, keys.length);
          if (target !== null) {
            event.preventDefault();
            setCursorKey(keys[target]!);
          }
        }}
        className="pointer-events-none absolute z-10 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
        style={{
          left: Y_AXIS_WIDTH,
          top: CHART_MARGIN.top,
          width: frame.plotWidth,
          height: frame.height - CHART_MARGIN.top - CHART_MARGIN.bottom - X_AXIS_HEIGHT,
        }}
      >
        {keys.map((key, index) => (
          <div
            key={key}
            id={optionId(index)}
            role="option"
            aria-selected={index === selectedAt}
            className={
              // The chip: pinned to the visible start of the plot, clear of
              // the Y axis pinned over it, so it stays in view however far the
              // plot has scrolled.
              focused && index === activeIndex
                ? "sticky mt-1 w-max max-w-full rounded-md border bg-popover px-2 py-1 text-xs text-popover-foreground shadow-md"
                : "sr-only"
            }
            style={focused && index === activeIndex ? { left: Y_AXIS_WIDTH + 4 } : undefined}
          >
            {selection.describeBucket(key)}
          </div>
        ))}
      </div>
    );

  return {
    overlay,
    selectedIndex: selectedAt >= 0 ? selectedAt : null,
    cursorIndex: focused && activeIndex >= 0 ? activeIndex : null,
    // The click is placed against the chart's own box, read in the handler:
    // an event handler may measure, having nothing to re-render into.
    onChartClick: (_state, event) => {
      const offset = event.clientX - event.currentTarget.getBoundingClientRect().left - Y_AXIS_WIDTH;
      const index = bucketIndexAt(offset, {
        count: keys.length,
        plotWidth: frame.plotWidth,
        edgePadding: xAxisEdgePadding(data.map((datum) => datum.label)),
        shape: frame.shape,
      });
      if (index !== null) {
        choose(keys[index]!);
      }
    },
  };
}
