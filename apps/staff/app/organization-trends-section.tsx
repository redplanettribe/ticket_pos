"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { toAppLocale, type AppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  CHART_PLOT_INSET,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  ChartScrollArea,
  ChartViewToggle,
  Input,
  Label,
  MultiSeriesLineChart,
  Skeleton,
  StackedAreaChart,
  StackedBarChart,
  TrendTile,
  chartSeriesColor,
  type ChartBucketSelection,
  type ChartViewOption,
  type StackedSeries,
  type TrendTileChangeCopy,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatCalendarDay, formatMoney, formatNumber } from "@/lib/format";
import {
  ORGANIZATION_TREND_COMPARISONS,
  ORGANIZATION_TREND_SPANS,
  countedNothing,
  cumulativeStackData,
  dailyStackData,
  fetchOrganizationTrends,
  formatOrganizationTrendBucket,
  organizationTrendTile,
  organizationTrendsQuery,
  organizationTrendsScrollAnchor,
  organizationTrendsViewAfterComparisonChange,
  organizationTrendsViewAfterRangeChange,
  organizationTrendsViewAfterSpanChange,
  type OrganizationTrendComparison,
  type OrganizationTrendDays,
  type OrganizationTrendFigure,
  type OrganizationTrendSpan,
  type OrganizationTrends,
  type OrganizationTrendsView,
} from "@/lib/organization-trends";
import {
  organizationBucketSelectionIn,
  type OrganizationTrendBucketSelection,
  type OrganizationTrendFigureId,
} from "@/lib/organization-trends-breakdown";
import {
  STACK_COMPARISON_ID,
  formatTrendChange,
  trendChartYMax,
  type TrendStackDatum,
} from "@/lib/platform-trends";
import { cumulativePlotWidth, formatTakingsTick, trendsYTicks, type TrendsView } from "@/lib/sales-trends";
import { useElementWidth } from "@/lib/use-element-width";

import { OrganizationTrendBreakdownPanel } from "./organization-trends-breakdown";

/** Home's own path: the reading's address is this plus its query. */
const HOME_PATH = "/";

const SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-60";

/** One column on a phone, two tiles to a row from sm. */
export const ORGANIZATION_TRENDS_TILES_GRID = "grid grid-cols-1 gap-4 sm:grid-cols-2";

const SPAN_KEYS = {
  "7d": "span7d",
  "30d": "span30d",
  "90d": "span90d",
  "12m": "span12m",
  all: "spanAll",
  custom: "spanCustom",
} as const satisfies Record<OrganizationTrendSpan, string>;

const COMPARISON_KEYS = {
  previous: "comparePrevious",
  year: "compareYear",
} as const satisfies Record<OrganizationTrendComparison, string>;

/** The Sales Channels by their keys in `sales`, which coined the words. A
 * channel the API adds before this app names it is shown by its own key. */
const CHANNEL_KEYS: Record<string, "channelOnline" | "channelInPerson" | "channelImport"> = {
  online: "channelOnline",
  in_person: "channelInPerson",
  import: "channelImport",
};

/** The line a stack is compared with stands for every channel, so it takes
 * none of their colours: the text colour, as on Platform Trends. */
const STACK_COMPARISON_COLOR = "var(--foreground)";

const CHART_HEIGHT = 240;

/** The figures a chart is drawn for, by their names in the API, which a
 * bucket's breakdown is asked for by. */
const FIGURE_IDS = {
  takings: "takings",
  ticketsSold: "tickets_sold",
  pageViews: "page_views",
  eventsHeld: "events_held",
} as const satisfies Record<string, OrganizationTrendFigureId>;

/** What a chart's choosable buckets are drawn as, for focus to return to. */
const BUCKET_LISTBOX = '[role="listbox"]';

/**
 * Organization Trends (#861, #862, #863, spec #858, ADR 0081), beneath what stands
 * now on Home.
 *
 * The reading on screen is named by the page's address (`view`, parsed from
 * it on the server), and every choice is a push of a new address, so a
 * reload or a shared link shows the same reading. The section asks for that
 * reading in one request and draws what it is told. It reads on its own,
 * apart from what stands now, and a failure is said inside the section, with
 * the controls left on screen to put a refused range right.
 */
export function OrganizationTrendsSection({ view }: { view: OrganizationTrendsView }) {
  const t = useTranslations("organizationTrends");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const router = useRouter();
  const [trends, setTrends] = useState<OrganizationTrends | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  const query = organizationTrendsQuery(view);
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    fetchOrganizationTrends(view)
      .then((data) => {
        if (!cancelled) setTrends(data);
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setTrends(null);
          setLoadError((error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ?? t("loadFailed"));
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);

  const show = (next: OrganizationTrendsView) => {
    router.push(`${HOME_PATH}${organizationTrendsQuery(next)}`, { scroll: false });
  };

  return (
    <section aria-labelledby="organization-trends-title" className="space-y-4">
      <div className="space-y-1">
        <h2 id="organization-trends-title" className="text-xl font-semibold tracking-tight">
          {t("title")}
        </h2>
        <p className="text-sm text-muted-foreground">
          {t("description", { timezone: trends?.timezone ?? PLATFORM_TIME_ZONE })}
        </p>
      </div>

      <OrganizationTrendsPicker view={view} shown={trends?.span ?? null} waiting={trends === null || loading} onShow={show} />

      {loadError ? (
        <Alert variant="destructive">
          <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
          <AlertDescription>{loadError}</AlertDescription>
        </Alert>
      ) : trends === null ? (
        <div role="status" aria-label={t("loading")}>
          <Skeleton className="h-80 w-full" />
        </div>
      ) : (
        // The reading before stays on screen, dimmed, while the next loads.
        <div aria-busy={loading} className={loading ? "space-y-4 opacity-60" : "space-y-4"}>
          <OrganizationTrendsReading trends={trends} locale={locale} />
        </div>
      )}
    </section>
  );
}

/**
 * The span and the comparison. All time has no span before it, so the
 * comparison is shown disabled and says so.
 */
export function OrganizationTrendsPicker({
  view,
  shown,
  waiting,
  onShow,
}: {
  view: OrganizationTrendsView;
  /** The span on screen, which a custom range starts from. */
  shown: OrganizationTrends["span"] | null;
  /** True while the reading the address names is not yet on screen. */
  waiting: boolean;
  onShow: (next: OrganizationTrendsView) => void;
}) {
  const t = useTranslations("organizationTrends");
  const allTime = view.span === "all";
  return (
    <div className="grid grid-cols-1 gap-3 rounded-md border p-4 sm:grid-cols-2 lg:grid-cols-4">
      <div className="flex flex-col gap-1">
        <Label htmlFor="organization-trends-span">{t("spanLabel")}</Label>
        <select
          id="organization-trends-span"
          className={SELECT_CLASS}
          value={view.span}
          onChange={(event) =>
            onShow(organizationTrendsViewAfterSpanChange(view, event.target.value as OrganizationTrendSpan, shown))
          }
        >
          {ORGANIZATION_TREND_SPANS.map((span) => (
            // Custom starts from the span on screen, so it waits for it.
            <option key={span} value={span} disabled={span === "custom" && view.span !== "custom" && waiting}>
              {t(SPAN_KEYS[span])}
            </option>
          ))}
        </select>
      </div>
      {view.span === "custom" ? (
        <CustomRangeForm
          from={view.from}
          to={view.to}
          onApply={(range) => onShow(organizationTrendsViewAfterRangeChange(view, range))}
        />
      ) : null}
      <div className="flex flex-col gap-1">
        <Label htmlFor="organization-trends-compare">{t("compareLabel")}</Label>
        <select
          id="organization-trends-compare"
          className={SELECT_CLASS}
          value={allTime ? "none" : view.compare}
          disabled={allTime}
          onChange={(event) =>
            onShow(organizationTrendsViewAfterComparisonChange(view, event.target.value as OrganizationTrendComparison))
          }
        >
          {allTime ? <option value="none">{t("compareNone")}</option> : null}
          {ORGANIZATION_TREND_COMPARISONS.map((comparison) => (
            <option key={comparison} value={comparison}>
              {t(COMPARISON_KEYS[comparison])}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

/**
 * The two days of a custom range, applied by its button and not as a day
 * changes, as on Platform Trends: a year typed into a date input passes
 * through whole, valid days on its way, and each would be a new address.
 */
function CustomRangeForm({
  from,
  to,
  onApply,
}: {
  from: string;
  to: string;
  onApply: (range: { from: string; to: string }) => void;
}) {
  const t = useTranslations("organizationTrends");
  const [range, setRange] = useState({ from, to });
  useEffect(() => {
    setRange({ from, to });
  }, [from, to]);
  return (
    <form
      className="grid grid-cols-1 gap-3 sm:col-span-2 sm:grid-cols-[1fr_1fr_auto] sm:items-end"
      onSubmit={(event) => {
        event.preventDefault();
        onApply(range);
      }}
    >
      <div className="flex flex-col gap-1">
        <Label htmlFor="organization-trends-from">{t("fromLabel")}</Label>
        <Input
          id="organization-trends-from"
          type="date"
          required
          value={range.from}
          max={range.to || undefined}
          onChange={(event) => setRange((current) => ({ ...current, from: event.target.value }))}
          className="h-9"
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="organization-trends-to">{t("toLabel")}</Label>
        <Input
          id="organization-trends-to"
          type="date"
          required
          value={range.to}
          min={range.from || undefined}
          onChange={(event) => setRange((current) => ({ ...current, to: event.target.value }))}
          className="h-9"
        />
      </div>
      <Button type="submit" variant="outline" size="sm" className="h-9">
        {t("applyRange")}
      </Button>
    </form>
  );
}

/**
 * One reading, drawn: a tile per figure, then the charts, with one Daily and
 * Cumulative choice for every chart of the section.
 *
 * Takings is drawn only when the reading carries it. The API withholds it
 * from Event Staff (#862), so the section decides nothing about money: what
 * it is not handed, it does not draw, and never as a zero.
 *
 * Page Views (#863) are loads, never people, and are drawn and said as such:
 * nothing here divides anything by them.
 *
 * Any bucket of a Daily chart opens into the Events that made it (#864),
 * beneath that chart, as on Platform Trends. The Cumulative view draws
 * running sums, which no Event made alone, so its buckets open nothing.
 */
export function OrganizationTrendsReading({
  trends,
  locale,
  initialChartView = "daily",
  initialSelection = null,
}: {
  trends: OrganizationTrends;
  locale: AppLocale;
  /** The view the charts open in (tests). */
  initialChartView?: TrendsView;
  /** The bucket whose breakdown is open at first (tests). */
  initialSelection?: OrganizationTrendBucketSelection | null;
}) {
  const t = useTranslations("organizationTrends");
  const tSales = useTranslations("sales");
  const [chartView, setChartView] = useState<TrendsView>(initialChartView);
  const { figures, comparison } = trends;

  // The bucket whose breakdown is open: one for the whole section, so
  // choosing a bucket on another chart moves the panel there. It is held with
  // the reading it was chosen on and let go when another reading arrives, so
  // a day chosen on the span before is not left open under other buckets.
  const [chosen, setChosen] = useState<{ trends: OrganizationTrends; selection: OrganizationTrendBucketSelection } | null>(
    initialSelection === null ? null : { trends, selection: initialSelection },
  );
  const selected = chosen !== null && chosen.trends === trends && chartView === "daily" ? chosen.selection : null;
  const onSelect = useCallback(
    (selection: OrganizationTrendBucketSelection) => setChosen({ trends, selection }),
    [trends],
  );
  const choice = { selected, onSelect };
  const breakdownOf = (figureId: OrganizationTrendFigureId, formatValue: (value: number) => string) =>
    selected?.figure === figureId ? { selection: selected, formatValue, onClose: () => setChosen(null) } : undefined;
  const bucketName = (selection: OrganizationTrendBucketSelection) =>
    formatOrganizationTrendBucket(
      {
        first_day: selection.firstDay,
        last_day: selection.lastDay,
        partial: trends.span.buckets.find((bucket) => bucket.first_day === selection.firstDay)?.partial ?? false,
      },
      trends.granularity,
      locale,
    );

  const count = (value: number) => formatNumber(value, locale);
  const money = (cents: number) => formatMoney(cents, trends.currency, locale);
  const moneyTick = (cents: number) => formatTakingsTick(cents, trends.currency, locale);
  const takings = figures.takings ?? null;
  const changeCopy: TrendTileChangeCopy = {
    format: (ratio) => formatTrendChange(ratio, locale),
    growth: t("changeGrowth"),
    decline: t("changeDecline"),
    unchanged: t("changeUnchanged"),
    nothingToCompare: t("nothingToCompare"),
  };
  const tile = (id: string, label: string, figure: OrganizationTrendFigure, format: (value: number) => string) => {
    const reading = organizationTrendTile(figure, comparison !== null, format);
    return {
      id,
      label,
      readings: [
        {
          id: reading.id,
          value: reading.value ?? format(0),
          change: reading.change,
          noChange: reading.noChange === undefined ? undefined : t(reading.noChange),
          trend: reading.trend,
        },
      ],
    };
  };
  const tiles = [
    ...(takings === null ? [] : [tile("takings", t("takings"), takings, money)]),
    tile("tickets_sold", t("ticketsSold"), figures.tickets_sold, count),
    tile("ticket_sales", t("ticketSales"), figures.ticket_sales, count),
    tile("page_views", t("pageViews"), figures.page_views, count),
    tile("events_held", t("eventsHeld"), figures.events_held, count),
  ];

  const viewOptions: ChartViewOption<TrendsView>[] = useMemo(
    () => [
      { id: "daily", label: t("viewDaily") },
      { id: "cumulative", label: t("viewCumulative") },
    ],
    [t],
  );
  const channelName = (key: string) => {
    const messageKey = CHANNEL_KEYS[key];
    return messageKey === undefined ? key : tSales(messageKey);
  };

  const reading = {
    from: formatCalendarDay(trends.span.first_day, locale),
    to: formatCalendarDay(trends.span.last_day, locale),
    granularity: trends.granularity,
  };

  return (
    <>
      <p className="text-sm text-muted-foreground">
        {comparison === null
          ? t("reading", reading)
          : t("readingCompared", {
              ...reading,
              compareFrom: formatCalendarDay(comparison.first_day, locale),
              compareTo: formatCalendarDay(comparison.last_day, locale),
            })}
      </p>

      <div role="group" aria-label={t("tilesLabel")} className={ORGANIZATION_TRENDS_TILES_GRID}>
        {tiles.map((entry) => (
          <TrendTile key={entry.id} label={entry.label} readings={entry.readings} changeCopy={changeCopy} />
        ))}
      </div>

      <p className="text-xs text-muted-foreground">{t("notFinalNote")}</p>

      <div className="flex justify-end">
        <ChartViewToggle options={viewOptions} value={chartView} onChange={setChartView} ariaLabel={t("viewLabel")} />
      </div>

      <div role="group" aria-label={t("chartsLabel")} className="grid grid-cols-1 gap-4">
        {takings === null ? null : (
          <FigureCard
            title={t("takings")}
            description={chartView === "cumulative" ? t("takingsDescriptionCumulative") : t("takingsDescription")}
            breakdown={breakdownOf(FIGURE_IDS.takings, money)}
            bucketName={bucketName}
          >
            <StackedFigureChart
              trends={trends}
              figureId={FIGURE_IDS.takings}
              {...choice}
              source={takings}
              title={t("takings")}
              totalLabel={t("takingsTotal")}
              formatValue={money}
              formatTickValue={moneyTick}
              view={chartView}
              locale={locale}
              channelName={channelName}
            />
          </FigureCard>
        )}
        <FigureCard
          title={t("ticketsSold")}
          description={chartView === "cumulative" ? t("ticketsSoldDescriptionCumulative") : t("ticketsSoldDescription")}
          breakdown={breakdownOf(FIGURE_IDS.ticketsSold, count)}
          bucketName={bucketName}
        >
          <StackedFigureChart
            trends={trends}
            figureId={FIGURE_IDS.ticketsSold}
            {...choice}
            source={figures.tickets_sold}
            title={t("ticketsSold")}
            totalLabel={t("ticketsTotal")}
            formatValue={count}
            view={chartView}
            locale={locale}
            channelName={channelName}
          />
          {countedNothing(figures.tickets_sold) ? (
            <p className="text-sm text-muted-foreground">{t("nothingSold")}</p>
          ) : null}
        </FigureCard>
        <FigureCard
          title={t("pageViews")}
          description={chartView === "cumulative" ? t("pageViewsDescriptionCumulative") : t("pageViewsDescription")}
          breakdown={breakdownOf(FIGURE_IDS.pageViews, count)}
          bucketName={bucketName}
        >
          <LineFigureChart
            trends={trends}
            figureId={FIGURE_IDS.pageViews}
            {...choice}
            source={figures.page_views}
            title={t("pageViews")}
            formatValue={count}
            view={chartView}
            locale={locale}
          />
          {countedNothing(figures.page_views) ? (
            <p className="text-sm text-muted-foreground">{t("nothingLoaded")}</p>
          ) : null}
        </FigureCard>
        <FigureCard
          title={t("eventsHeld")}
          description={chartView === "cumulative" ? t("eventsHeldDescriptionCumulative") : t("eventsHeldDescription")}
          breakdown={breakdownOf(FIGURE_IDS.eventsHeld, count)}
          bucketName={bucketName}
        >
          <StackedFigureChart
            trends={trends}
            figureId={FIGURE_IDS.eventsHeld}
            {...choice}
            source={figures.events_held}
            title={t("eventsHeld")}
            totalLabel={t("eventsHeldTotal")}
            formatValue={count}
            view={chartView}
            locale={locale}
            channelName={() => t("eventsHeld")}
          />
          {countedNothing(figures.events_held) ? (
            <p className="text-sm text-muted-foreground">{t("nothingHeld")}</p>
          ) : null}
        </FigureCard>
      </div>
    </>
  );
}

/** The open breakdown of a figure, as its card draws it beneath the chart. */
type FigureBreakdown = {
  selection: OrganizationTrendBucketSelection;
  formatValue: (value: number) => string;
  onClose: () => void;
};

/** What lets a chart's buckets be chosen: the bucket open on whichever chart
 * it was chosen on, and what a choice opens. */
type BucketChoice = {
  figureId: OrganizationTrendFigureId;
  selected: OrganizationTrendBucketSelection | null;
  onSelect: (selection: OrganizationTrendBucketSelection) => void;
};

/**
 * One figure's card: its chart, and beneath it the Events of the bucket
 * chosen on that chart, while one is.
 */
function FigureCard({
  title,
  description,
  breakdown,
  bucketName,
  children,
}: {
  title: string;
  description: string;
  breakdown?: FigureBreakdown;
  bucketName: (selection: OrganizationTrendBucketSelection) => string;
  children: ReactNode;
}) {
  const content = useRef<HTMLDivElement>(null);
  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent ref={content} className="space-y-3">
        {children}
        {breakdown === undefined ? null : (
          <OrganizationTrendBreakdownPanel
            selection={breakdown.selection}
            figure={title}
            bucket={bucketName(breakdown.selection)}
            formatValue={breakdown.formatValue}
            onClose={() => {
              // The Close button goes with the panel. Focus returns to the
              // chart the bucket was chosen on, so a keyboard reader is not
              // dropped at the top of the page.
              content.current?.querySelector<HTMLElement>(BUCKET_LISTBOX)?.focus();
              breakdown.onClose();
            }}
          />
        )}
      </CardContent>
    </Card>
  );
}

/**
 * What lets a Daily chart's buckets be chosen (#864): which of them is
 * selected, what a choice opens, and how each is read aloud. Choosing the
 * selected bucket again keeps it open; the panel is closed by its own button.
 */
function chartSelection(
  data: readonly TrendStackDatum[],
  { figureId, selected, onSelect }: BucketChoice,
  label: string,
  describe: (datum: TrendStackDatum) => string,
): ChartBucketSelection {
  const find = (key: string) => data.find((datum) => datum.key === key);
  return {
    selectedKey: organizationBucketSelectionIn(selected, figureId, data),
    onSelect: (key) => {
      const datum = find(key);
      if (datum !== undefined) {
        onSelect({ figure: figureId, key, firstDay: datum.firstDay, lastDay: datum.lastDay });
      }
    },
    label,
    describeBucket: (key) => {
      const datum = find(key);
      return datum === undefined ? key : describe(datum);
    },
  };
}

/** How a Daily chart's bucket is read aloud: its name and its value (a
 * stack's height, or a line's point), said partial for today and a month
 * the span cuts. */
function describeBucketValue(
  t: ReturnType<typeof useTranslations<"organizationTrends">>,
  format: (value: number) => string,
): (datum: TrendStackDatum) => string {
  return (datum) =>
    t(datum.partial ? "bucketPartial" : "bucketReading", { bucket: datum.label, value: format(datum.total ?? 0) });
}

/**
 * A figure drawn as bars: Takings and Tickets Sold, stacked by Sales Channel
 * in Platform Trends' channel colours, and Events held, one series.
 * The Daily view draws a bar per bucket, the comparison span's stack height
 * as the faint line and today, and any month the span cuts, as a faint bar.
 * The Cumulative view draws each channel's running sum from zero at the
 * span's start as stacked bands, ending at the span's total.
 */
function StackedFigureChart({
  trends,
  figureId,
  selected,
  onSelect,
  source,
  title: figureName,
  totalLabel,
  formatValue: format,
  formatTickValue,
  view,
  locale,
  channelName,
}: BucketChoice & {
  trends: OrganizationTrends;
  source: OrganizationTrendFigure;
  /** The figure's name, which the chart's labels carry. */
  title: string;
  totalLabel: string;
  /** How a value is written in the hover card, and on the axis unless
   * formatTickValue says otherwise. */
  formatValue: (value: number) => string;
  formatTickValue?: (value: number) => string;
  view: TrendsView;
  locale: AppLocale;
  channelName: (key: string) => string;
}) {
  const t = useTranslations("organizationTrends");
  const label = useCallback(
    (bucket: OrganizationTrendDays) => formatOrganizationTrendBucket(bucket, trends.granularity, locale),
    [trends.granularity, locale],
  );
  const cumulative = view === "cumulative";
  const data = useMemo(
    () =>
      cumulative
        ? cumulativeStackData(source, trends.span.buckets, label)
        : dailyStackData(source, trends.span.buckets, label),
    [cumulative, source, trends.span.buckets, label],
  );
  // The channels keep the order and the colours Platform Trends gives them.
  const series: StackedSeries[] = source.series.map((entry, index) => ({
    id: entry.key,
    name: channelName(entry.key),
    color: chartSeriesColor(index),
  }));
  const yMax = useMemo(() => trendChartYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  const ariaLabel = cumulative
    ? t("chartLabelCumulative", { figure: figureName, granularity: trends.granularity })
    : t("chartLabel", { figure: figureName, granularity: trends.granularity });

  return (
    <>
      {/* A figure of one series is named by its card, and has no key. */}
      {series.length > 1 ? (
        <ul aria-label={t("channelsLabel")} className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {series.map((entry) => (
            <li key={entry.id} className="flex items-center gap-1.5">
              <span aria-hidden className="size-2.5 rounded-sm" style={{ backgroundColor: entry.color }} />
              {entry.name}
            </li>
          ))}
        </ul>
      ) : null}
      <FigureChartWindow trends={trends} figureName={figureName} view={view}>
        {(plotWidth) =>
          cumulative ? (
            <StackedAreaChart
              data={data}
              series={series}
              yMax={yMax}
              yTicks={yTicks}
              plotWidth={plotWidth}
              height={CHART_HEIGHT}
              formatValue={format}
              formatTickValue={formatTickValue}
              totalLabel={totalLabel}
              ariaLabel={ariaLabel}
            />
          ) : (
            <StackedBarChart
              data={data}
              series={series}
              comparisonSeries={comparisonLine(trends, t, STACK_COMPARISON_COLOR)}
              yMax={yMax}
              yTicks={yTicks}
              plotWidth={plotWidth}
              height={CHART_HEIGHT}
              formatValue={format}
              formatTickValue={formatTickValue}
              totalLabel={totalLabel}
              partialLabel={t("partial")}
              selection={chartSelection(
                data,
                { figureId, selected, onSelect },
                t("selectBucket", { figure: figureName, granularity: trends.granularity }),
                describeBucketValue(t, format),
              )}
              ariaLabel={ariaLabel}
            />
          )
        }
      </FigureChartWindow>
    </>
  );
}

/**
 * The one line a Daily chart sets beside its buckets: the comparison span's
 * value, named by its kind. All time has none.
 */
function comparisonLine(
  trends: OrganizationTrends,
  t: ReturnType<typeof useTranslations<"organizationTrends">>,
  color: string,
): StackedSeries[] | undefined {
  if (trends.comparison === null) return undefined;
  return [
    {
      id: STACK_COMPARISON_ID,
      name: t(COMPARISON_KEYS[trends.comparison.kind] ?? "comparePrevious"),
      color,
    },
  ];
}

/**
 * The window a chart is drawn in, at its card's width, scrolling sideways
 * when the card is narrower than the floor and opening on its newest end once
 * per reading and view.
 */
function FigureChartWindow({
  trends,
  figureName,
  view,
  children,
}: {
  trends: OrganizationTrends;
  figureName: string;
  view: TrendsView;
  children: (plotWidth: number) => ReactNode;
}) {
  const t = useTranslations("organizationTrends");
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotWidth = cumulativePlotWidth(availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0);
  return (
    <ChartScrollArea
      ref={setScrollArea}
      ariaLabel={t("scrollLabel", { figure: figureName })}
      openAtNewestEnd={{ key: organizationTrendsScrollAnchor(trends, view), contentWidth: plotWidth }}
    >
      {children(plotWidth)}
    </ChartScrollArea>
  );
}

/**
 * A figure of one series drawn as a line (Page Views). The Daily view draws
 * each bucket's value, the comparison span's as the faint line, and today,
 * and any month the span cuts, as the dashed stretch; the Cumulative view
 * draws the running sum from zero at the span's start, ending at the span's
 * total, with no comparison. It reads the buckets the stacked charts read: a
 * stack of one series is that series' value.
 */
function LineFigureChart({
  trends,
  figureId,
  selected,
  onSelect,
  source,
  title: figureName,
  formatValue: format,
  view,
  locale,
}: BucketChoice & {
  trends: OrganizationTrends;
  source: OrganizationTrendFigure;
  /** The figure's name, which the chart's labels carry. */
  title: string;
  formatValue: (value: number) => string;
  view: TrendsView;
  locale: AppLocale;
}) {
  const t = useTranslations("organizationTrends");
  const label = useCallback(
    (bucket: OrganizationTrendDays) => formatOrganizationTrendBucket(bucket, trends.granularity, locale),
    [trends.granularity, locale],
  );
  const cumulative = view === "cumulative";
  const data = useMemo(
    () =>
      cumulative
        ? cumulativeStackData(source, trends.span.buckets, label)
        : dailyStackData(source, trends.span.buckets, label),
    [cumulative, source, trends.span.buckets, label],
  );
  const series: StackedSeries[] = [{ id: source.series[0]?.key ?? "all", name: figureName, color: chartSeriesColor(0) }];
  const yMax = useMemo(() => trendChartYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  const ariaLabel = cumulative
    ? t("chartLabelCumulative", { figure: figureName, granularity: trends.granularity })
    : t("chartLabel", { figure: figureName, granularity: trends.granularity });

  return (
    <FigureChartWindow trends={trends} figureName={figureName} view={view}>
      {(plotWidth) => (
        <MultiSeriesLineChart
          data={data}
          series={series}
          comparisonSeries={cumulative ? undefined : comparisonLine(trends, t, chartSeriesColor(0))}
          yMax={yMax}
          yTicks={yTicks}
          plotWidth={plotWidth}
          height={CHART_HEIGHT}
          formatValue={format}
          // A bucket nobody loaded is a zero the hover card names.
          zeroIsMeasured
          nothingCountedLabel={t("nothingCounted")}
          partialLabel={t("partial")}
          selection={
            cumulative
              ? undefined
              : chartSelection(
                  data,
                  { figureId, selected, onSelect },
                  t("selectBucket", { figure: figureName, granularity: trends.granularity }),
                  describeBucketValue(t, format),
                )
          }
          ariaLabel={ariaLabel}
        />
      )}
    </FigureChartWindow>
  );
}
