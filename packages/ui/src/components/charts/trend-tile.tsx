import * as React from "react";
import { ArrowDownRight, ArrowUpRight, Minus } from "lucide-react";

import { chartSeriesColor } from "../../lib/chart-palette";
import { cn } from "../../lib/utils";
import { roundedTrendChange, sparklineShape, trendDirection, type TrendDirection } from "./trend-tile-reading";

/**
 * One figure of a tile: what it came to over the span, how that compares, and
 * the shape of the span.
 *
 * A tile usually has one. A money tile has one per currency, since money is
 * never converted and so never summed across currencies: each currency is its
 * own figure with its own change and its own trend.
 */
export type TrendTileReading = {
  id: string;
  /** What tells this reading from the tile's others, e.g. a currency code.
   * Omitted when the tile has one reading. */
  caption?: string;
  /** The figure for the span, as the caller spells it. */
  value: string;
  /**
   * The change against the comparison span as a signed ratio (0.25 is a
   * quarter more), or null when there is nothing to compare against. Leave it
   * out altogether for a reading that states no change at all, such as a
   * figure that is not recorded.
   */
  change?: number | null;
  /**
   * What is said in place of a null change, when the caller knows why it is
   * absent (e.g. the comparison came to zero). Left out, the tile says
   * `changeCopy.nothingToCompare`.
   */
  noChange?: string;
  /** The figure bucket by bucket, oldest first, for the small trend line.
   * A null is a bucket with no value and breaks the line. */
  trend?: readonly (number | null)[];
};

/** A lesser figure stated under a tile's readings, e.g. the count of
 * different Customers active over the last 7 days. */
export type TrendTileSecondary = {
  id: string;
  label: string;
  value: string;
};

/**
 * The words of a change, which are the caller's: the staff app translates
 * them, and this package holds no copy.
 */
export type TrendTileChangeCopy = {
  /**
   * Spells a signed ratio as a signed percentage, e.g. 0.125 as "+12.5%".
   * The tile hands it the ratio already rounded to the one decimal of a
   * percent it states (`roundedTrendChange`), the number its direction was
   * read from. Print it at that precision and no coarser.
   */
  format: (ratio: number) => string;
  /** What a positive change is called where the arrow cannot be seen. */
  growth: string;
  /** What a negative change is called where the arrow cannot be seen. */
  decline: string;
  /** What a change that prints as zero is called where the dash cannot be
   * seen. */
  unchanged: string;
  /** What is said when there is no change to state, unless the reading says
   * why in its own `noChange`. */
  nothingToCompare: string;
};

export type TrendTileProps = {
  /** What the figure is, e.g. "Tickets Sold". */
  label: string;
  readings: TrendTileReading[];
  changeCopy: TrendTileChangeCopy;
  secondary?: TrendTileSecondary[];
  /** The trend line's colour. The first of the shared chart palette unless a
   * surface draws the same figure in another colour on its chart. */
  color?: string;
  className?: string;
};

/**
 * TrendTile states one figure of a trend reading at a glance: its label, what
 * it came to over the span, the change against the comparison span, and a
 * small line for the shape of the span.
 *
 * It draws what it is told and derives one thing, the direction of a change
 * from the sign of the percentage it prints (`trendDirection`): a change that
 * rounds to 0% is no change. The four states of a change are told
 * apart by an icon and by words, the percentage carries its own sign, and the
 * figure is in the text colour throughout, so no state is carried by colour
 * alone: growth is an arrow up and a "+", decline an arrow down and a "-", no
 * change a dash, and nothing to compare against a sentence and no icon.
 *
 * The trend line is a plain SVG stretched to the tile's width by its viewBox.
 * It is not a chart: no axis, no hover and no measuring of its own size, which
 * is what lets it sit in a server-rendered tile.
 */
export function TrendTile({ label, readings, changeCopy, secondary, color, className }: TrendTileProps) {
  const stroke = color ?? chartSeriesColor(0);
  // Named by its visible label, so the name is read once and not twice.
  const labelId = React.useId();
  return (
    <div
      role="group"
      aria-labelledby={labelId}
      className={cn(
        "flex min-w-0 flex-col gap-3 rounded-lg border bg-card p-4 text-card-foreground shadow-sm",
        className,
      )}
    >
      <p id={labelId} className="text-sm font-medium text-muted-foreground">
        {label}
      </p>
      {readings.map((reading) => (
        <div key={reading.id} className="min-w-0 space-y-1">
          {/* The caption has a line of its own, under the figure: beside it,
              it would drop under a wide figure and not under a narrow one, so
              the tile's height would follow the figure (#840). */}
          <p className="min-w-0">
            <span className="block text-2xl font-semibold leading-tight tabular-nums break-words">
              {reading.value}
            </span>
            {reading.caption === undefined ? null : (
              <span className="block text-xs font-medium text-muted-foreground">{reading.caption}</span>
            )}
          </p>
          {reading.change === undefined ? null : (
            <TrendTileChange change={reading.change} noChange={reading.noChange} copy={changeCopy} />
          )}
          {reading.trend === undefined ? null : <TrendTileSparkline values={reading.trend} stroke={stroke} />}
        </div>
      ))}
      {secondary === undefined || secondary.length === 0 ? null : (
        // One to a row, whatever the figures: side by side, two short ones
        // fit on a row and two long ones do not, and the tile's height would
        // change with the span (#840).
        <dl className="mt-auto grid gap-y-1 border-t pt-3 text-xs">
          {secondary.map((entry) => (
            <div key={entry.id} className="flex items-baseline gap-1.5">
              <dt className="text-muted-foreground">{entry.label}</dt>
              <dd className="font-medium tabular-nums">{entry.value}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}

const DIRECTION_ICON = {
  growth: ArrowUpRight,
  decline: ArrowDownRight,
  unchanged: Minus,
} as const;

/** The icon's colour, the one place a tile colours a state. It repeats what
 * the icon's shape, the sign and the words already say. */
const DIRECTION_TONE: Record<Exclude<TrendDirection, "nothing">, string> = {
  growth: "text-success",
  decline: "text-destructive",
  unchanged: "text-muted-foreground",
};

function TrendTileChange({
  change,
  noChange,
  copy,
}: {
  change: number | null;
  noChange: string | undefined;
  copy: TrendTileChangeCopy;
}) {
  // One rounding for both: the direction is the sign of the number printed.
  const stated = roundedTrendChange(change);
  const direction = trendDirection(stated);
  if (direction === "nothing" || stated === null) {
    return <p className="text-xs text-muted-foreground">{noChange ?? copy.nothingToCompare}</p>;
  }
  const Icon = DIRECTION_ICON[direction];
  return (
    <p className="flex items-center gap-1 text-xs font-medium tabular-nums">
      <Icon aria-hidden className={cn("size-3.5 shrink-0", DIRECTION_TONE[direction])} />
      <span className="sr-only">{copy[direction]}</span>
      <span>{copy.format(stated)}</span>
    </p>
  );
}

/** The box the trend line is worked out in. It is stretched to the tile's
 * width when drawn, so only the proportions matter. */
const SPARKLINE_BOX = { width: 120, height: 32, inset: 3 } as const;

/**
 * The small trend line. Hidden from assistive technology: it restates, as a
 * shape, the span the figure and its change already put in words.
 *
 * `preserveAspectRatio="none"` lets the one drawing fill a tile of any width,
 * and `vector-effect` keeps the stroke its own width while the box stretches.
 * A value alone between two gaps is a zero-length line with a round cap, which
 * that same effect draws as a round dot whatever the stretch.
 */
function TrendTileSparkline({ values, stroke }: { values: readonly (number | null)[]; stroke: string }) {
  const { runs, points } = sparklineShape(values, SPARKLINE_BOX);
  return (
    <svg
      aria-hidden
      viewBox={`0 0 ${SPARKLINE_BOX.width} ${SPARKLINE_BOX.height}`}
      preserveAspectRatio="none"
      className="block h-8 w-full"
      fill="none"
      stroke={stroke}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {runs.map((run) => (
        <polyline
          key={run[0]!.x}
          points={run.map((point) => `${point.x},${point.y}`).join(" ")}
          strokeWidth={2}
          vectorEffect="non-scaling-stroke"
        />
      ))}
      {points.map((point) => (
        <line
          key={point.x}
          x1={point.x}
          y1={point.y}
          x2={point.x}
          y2={point.y}
          strokeWidth={5}
          vectorEffect="non-scaling-stroke"
        />
      ))}
    </svg>
  );
}
