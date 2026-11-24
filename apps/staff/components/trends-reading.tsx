"use client";

// The pieces every trends reading of the staff app is drawn with: Platform
// Trends on the Operator Dashboard's Overview and Organization Trends on Home
// alike. The span picker, the window a chart is drawn in, the line chart of
// one figure, the key of a stack, a figure's card and what lets a chart's
// buckets be chosen.
//
// Each surface keeps its own words, figures and layout: every label here
// arrives translated, as `SortableHeader`'s does, and what differs between
// the two (the spans offered, the ids, a chart's height) is a prop.

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import {
  Button,
  CHART_PLOT_INSET,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  ChartScrollArea,
  Input,
  Label,
  MultiSeriesLineChart,
  type ChartBucketSelection,
  type ChartMarker,
  type StackedSeries,
} from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { cumulativePlotWidth, trendsYTicks } from "@/lib/sales-trends";
import { trendChartYMax, type TrendChartDatum } from "@/lib/trends-chart";
import {
  trendSpanViewAfterComparisonChange,
  trendSpanViewAfterRangeChange,
  trendSpanViewAfterSpanChange,
  type TrendDays,
  type TrendSpanScheme,
  type TrendSpanView,
} from "@/lib/trends-span";
import { useElementWidth } from "@/lib/use-element-width";

/** A select of the span picker, as tall as the inputs beside it. */
export const TRENDS_SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-60";

/** The colour of the line a stack is compared with. The stack has one colour
 * per Sales Channel and the line stands for all of them, so it takes none of
 * theirs: the text colour, which follows the theme. */
export const STACK_COMPARISON_COLOR = "var(--foreground)";

/** The keyboard surface the shared charts lay over a plot whose buckets can
 * be chosen, which focus returns to. */
export const BUCKET_LISTBOX = '[role="listbox"]';

/** The Sales Channels a stack is split by, by their keys in `sales`, which
 * coined the words: a trends reading reads them and never coins a second. */
const CHANNEL_KEYS: Record<string, "channelOnline" | "channelInPerson" | "channelImport"> = {
  online: "channelOnline",
  in_person: "channelInPerson",
  import: "channelImport",
};

/** How a Sales Channel is named. A channel the API adds before this app
 * names it is shown by its own key. */
export function useSalesChannelName(): (key: string) => string {
  const tSales = useTranslations("sales");
  return (key) => {
    const messageKey = CHANNEL_KEYS[key];
    return messageKey === undefined ? key : tSales(messageKey);
  };
}

// ---------------------------------------------------------------------------
// The span picker
// ---------------------------------------------------------------------------

/** The words of the span picker, in the surface's own catalog. */
export type TrendsSpanPickerCopy<Span extends string, Comparison extends string> = {
  spanLabel: string;
  compareLabel: string;
  /** What the comparison select says on the all time span, which has none.
   * Only a scheme with an all time span is asked for it. */
  noComparison?: string;
  fromLabel: string;
  toLabel: string;
  applyRange: string;
  spanName: (span: Span) => string;
  comparisonName: (comparison: Comparison) => string;
};

/**
 * The span and the comparison of a reading, and the two days of a custom
 * range. Every choice is handed to `onShow` as the next view, which the
 * section pushes as a new address. All time has no span before it, so on a
 * scheme that offers it the comparison is shown disabled and says so.
 */
export function TrendsSpanPicker<Span extends string, Comparison extends string>({
  scheme,
  view,
  shown,
  waiting,
  onShow,
  idPrefix,
  copy,
  oneColumnOnPhone = false,
}: {
  scheme: TrendSpanScheme<Span, Comparison>;
  view: TrendSpanView<Span, Comparison>;
  /** The span on screen, which a custom range starts from. */
  shown: TrendDays | null;
  /** True while the reading the address names is not yet on screen. */
  waiting: boolean;
  onShow: (next: TrendSpanView<Span, Comparison>) => void;
  /** Prefixes the controls' ids: `${idPrefix}-span` and the rest. */
  idPrefix: string;
  copy: TrendsSpanPickerCopy<Span, Comparison>;
  /** Sets the picker and the custom range to one column below sm, where the
   * grid otherwise sizes its one column to the widest control. */
  oneColumnOnPhone?: boolean;
}) {
  const allTime = scheme.allTime !== null && view.span === scheme.allTime;
  const phoneColumns = oneColumnOnPhone ? "grid-cols-1 " : "";
  return (
    <div className={`grid ${phoneColumns}gap-3 rounded-md border p-4 sm:grid-cols-2 lg:grid-cols-4`}>
      <div className="flex flex-col gap-1">
        <Label htmlFor={`${idPrefix}-span`}>{copy.spanLabel}</Label>
        <select
          id={`${idPrefix}-span`}
          className={TRENDS_SELECT_CLASS}
          value={view.span}
          onChange={(event) => onShow(trendSpanViewAfterSpanChange(scheme, view, event.target.value as Span, shown))}
        >
          {scheme.spans.map((span) => (
            // Custom starts from the span on screen, so it waits for the
            // reading the address names to be the one on screen.
            <option key={span} value={span} disabled={span === "custom" && view.span !== "custom" && waiting}>
              {copy.spanName(span)}
            </option>
          ))}
        </select>
      </div>
      {view.span === "custom" ? (
        <CustomRangeForm
          from={view.from}
          to={view.to}
          idPrefix={idPrefix}
          copy={copy}
          phoneColumns={phoneColumns}
          onApply={(range) => onShow(trendSpanViewAfterRangeChange(view, range))}
        />
      ) : null}
      <div className="flex flex-col gap-1">
        <Label htmlFor={`${idPrefix}-compare`}>{copy.compareLabel}</Label>
        <select
          id={`${idPrefix}-compare`}
          className={TRENDS_SELECT_CLASS}
          value={allTime ? "none" : view.compare}
          disabled={allTime}
          onChange={(event) => onShow(trendSpanViewAfterComparisonChange(view, event.target.value as Comparison))}
        >
          {allTime ? <option value="none">{copy.noComparison}</option> : null}
          {scheme.comparisons.map((comparison) => (
            <option key={comparison} value={comparison}>
              {copy.comparisonName(comparison)}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

/**
 * The two days of a custom range.
 *
 * Applied by its button and not as a day changes, the rule the Tax Invoices
 * search box follows: a year typed into a date input passes through whole,
 * valid days on its way (the year 2, the year 20), and each would be a new
 * address, a history entry and a request. The days in the inputs are the
 * form's own until applied, and are set again whenever the address moves
 * under it (the back button, a shared link).
 */
function CustomRangeForm({
  from,
  to,
  idPrefix,
  copy,
  phoneColumns,
  onApply,
}: {
  from: string;
  to: string;
  idPrefix: string;
  copy: Pick<TrendsSpanPickerCopy<string, string>, "fromLabel" | "toLabel" | "applyRange">;
  phoneColumns: string;
  onApply: (range: { from: string; to: string }) => void;
}) {
  const [range, setRange] = useState({ from, to });
  useEffect(() => {
    setRange({ from, to });
  }, [from, to]);
  return (
    <form
      className={`grid ${phoneColumns}gap-3 sm:col-span-2 sm:grid-cols-[1fr_1fr_auto] sm:items-end`}
      onSubmit={(event) => {
        event.preventDefault();
        onApply(range);
      }}
    >
      {/* Each day bounds the other. What else a range may be is the API's to
          refuse, and its refusal is shown below with this form still here. */}
      <div className="flex flex-col gap-1">
        <Label htmlFor={`${idPrefix}-from`}>{copy.fromLabel}</Label>
        <Input
          id={`${idPrefix}-from`}
          type="date"
          required
          value={range.from}
          max={range.to || undefined}
          onChange={(event) => setRange((current) => ({ ...current, from: event.target.value }))}
          className="h-9"
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor={`${idPrefix}-to`}>{copy.toLabel}</Label>
        <Input
          id={`${idPrefix}-to`}
          type="date"
          required
          value={range.to}
          min={range.from || undefined}
          onChange={(event) => setRange((current) => ({ ...current, to: event.target.value }))}
          className="h-9"
        />
      </div>
      <Button type="submit" variant="outline" size="sm" className="h-9">
        {copy.applyRange}
      </Button>
    </form>
  );
}

// ---------------------------------------------------------------------------
// A figure's card
// ---------------------------------------------------------------------------

/**
 * One figure's card: its chart, and beneath it the breakdown of the bucket
 * chosen on that chart, while one is. `breakdown` draws the panel, handed
 * what returns focus to the chart the bucket was chosen on, for its Close
 * button to call: a keyboard reader is not dropped at the top of the page.
 */
export function TrendFigureCard({
  title,
  titleClassName = "text-base",
  description,
  breakdown,
  children,
}: {
  title: string;
  /** The title's scale: a card's own, unless its page sets one for every
   * card on it (the Overview's). */
  titleClassName?: string;
  description: string;
  breakdown?: (returnFocus: () => void) => ReactNode;
  children: ReactNode;
}) {
  const content = useRef<HTMLDivElement>(null);
  // The listbox of the chart a bucket was last chosen on. A card may draw
  // more than one chart (one per currency), and Close returns to the one the
  // reader was on.
  const chosenOn = useRef<HTMLElement | null>(null);
  const remember = (target: EventTarget) => {
    // Up from what was clicked or keyed to the first thing that holds a
    // listbox, short of the card itself: the panel holds none and is passed
    // over, so its own buttons do not move the place.
    for (
      let element = target as Element | null;
      element !== null && element !== content.current;
      element = element.parentElement
    ) {
      const listbox = element.matches(BUCKET_LISTBOX) ? element : element.querySelector(BUCKET_LISTBOX);
      if (listbox !== null) {
        chosenOn.current = listbox as HTMLElement;
        return;
      }
    }
  };
  const returnFocus = () => {
    const chart = chosenOn.current?.isConnected
      ? chosenOn.current
      : content.current?.querySelector<HTMLElement>(BUCKET_LISTBOX);
    chart?.focus();
  };
  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle className={titleClassName}>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent
        ref={content}
        className="space-y-3"
        onClickCapture={(event) => remember(event.target)}
        onKeyDownCapture={(event) => remember(event.target)}
      >
        {children}
        {breakdown === undefined ? null : breakdown(returnFocus)}
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Charts
// ---------------------------------------------------------------------------

/**
 * What lets a chart's buckets be chosen: which of them is selected, what a
 * choice opens, and how each is read aloud. Choosing the selected bucket
 * again keeps it open; the panel is closed by its own button.
 */
export function chartSelection<Datum extends TrendChartDatum>(
  data: readonly Datum[],
  {
    selectedKey,
    onChoose,
  }: {
    /** The key of the bucket selected on this chart, or null. */
    selectedKey: string | null;
    /** Opens the breakdown of the bucket drawn as `datum`. */
    onChoose: (datum: Datum) => void;
  },
  label: string,
  describe: (datum: Datum) => string,
): ChartBucketSelection {
  const find = (key: string) => data.find((datum) => datum.key === key);
  return {
    selectedKey,
    onSelect: (key) => {
      const datum = find(key);
      if (datum !== undefined) {
        onChoose(datum);
      }
    },
    label,
    describeBucket: (key) => {
      const datum = find(key);
      return datum === undefined ? key : describe(datum);
    },
  };
}

/** The top of a chart's Y axis and its ticks, for the tallest thing drawn. */
export function useTrendYAxis(data: readonly (TrendChartDatum & { total?: number | null })[]): {
  yMax: number;
  yTicks: number[];
} {
  const yMax = useMemo(() => trendChartYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  return { yMax, yTicks };
}

/**
 * The window one chart is drawn in, and the width it is drawn at: the card's
 * own, so a span is looked at whole, as the Cumulative view of Sales Trends
 * is. A card narrower than the floor scrolls sideways instead of squeezing,
 * and opens on its latest days, once per `scrollAnchor` (#835).
 */
export function TrendChartWindow({
  ariaLabel,
  scrollAnchor,
  children,
}: {
  /** The scroll area's accessible name. */
  ariaLabel: string;
  /** Names the data drawn, so the chart opens at its newest end once per
   * reading (and view). */
  scrollAnchor: string;
  children: (plotWidth: number) => ReactNode;
}) {
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotWidth = cumulativePlotWidth(availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0);
  return (
    <ChartScrollArea ref={setScrollArea} ariaLabel={ariaLabel} openAtNewestEnd={{ key: scrollAnchor, contentWidth: plotWidth }}>
      {children(plotWidth)}
    </ChartScrollArea>
  );
}

/**
 * One figure drawn as one line in its window, with the comparison span as a
 * faint line beside it where one is given, and the partial stretch where the
 * data marks one. A bucket nothing happened in is a finding, and is named as
 * a zero beside a comparison that may not be one.
 */
export function TrendLineChart({
  scrollLabel,
  scrollAnchor,
  height,
  data,
  series,
  comparisonSeries,
  formatValue,
  formatTickValue,
  marker,
  notRecordedLabel,
  nothingCountedLabel,
  partialLabel,
  selection,
  ariaLabel,
}: {
  /** The accessible name of the chart's scroll area. */
  scrollLabel: string;
  scrollAnchor: string;
  /** How tall the chart is drawn, each surface's own. */
  height: number;
  data: TrendChartDatum[];
  series: StackedSeries[];
  comparisonSeries?: StackedSeries[];
  formatValue: (value: number) => string;
  formatTickValue?: (value: number) => string;
  marker?: ChartMarker;
  notRecordedLabel?: string;
  nothingCountedLabel: string;
  partialLabel: string;
  selection?: ChartBucketSelection;
  ariaLabel: string;
}) {
  const { yMax, yTicks } = useTrendYAxis(data);
  return (
    <TrendChartWindow ariaLabel={scrollLabel} scrollAnchor={scrollAnchor}>
      {(plotWidth) => (
        <MultiSeriesLineChart
          data={data}
          series={series}
          comparisonSeries={comparisonSeries}
          yMax={yMax}
          yTicks={yTicks}
          plotWidth={plotWidth}
          height={height}
          formatValue={formatValue}
          formatTickValue={formatTickValue}
          zeroIsMeasured
          nothingCountedLabel={nothingCountedLabel}
          partialLabel={partialLabel}
          marker={marker}
          selection={selection}
          notRecordedLabel={notRecordedLabel}
          ariaLabel={ariaLabel}
        />
      )}
    </TrendChartWindow>
  );
}

/** The key of a stack: a plain list of its series and their colours, not the
 * chips of Sales Trends, since nothing here is toggled. */
export function TrendSeriesKey({ label, series }: { label: string; series: readonly StackedSeries[] }) {
  return (
    <ul aria-label={label} className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {series.map((entry) => (
        <li key={entry.id} className="flex items-center gap-1.5">
          <span aria-hidden className="size-2.5 rounded-sm" style={{ backgroundColor: entry.color }} />
          {entry.name}
        </li>
      ))}
    </ul>
  );
}
