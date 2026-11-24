"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import { toAppLocale, type AppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  ChartViewToggle,
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

import {
  STACK_COMPARISON_COLOR,
  TrendChartWindow,
  TrendFigureCard,
  TrendLineChart,
  TrendSeriesKey,
  TrendsSpanPicker,
  chartSelection,
  useSalesChannelName,
  useTrendYAxis,
} from "@/components/trends-reading";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatCalendarDay, formatMoney, formatNumber } from "@/lib/format";
import {
  ORGANIZATION_TRENDS_SPAN_SCHEME,
  countedNothing,
  cumulativeStackData,
  dailyStackData,
  fetchOrganizationTrends,
  formatOrganizationTrendBucket,
  organizationTrendTile,
  organizationTrendsQuery,
  organizationTrendsScrollAnchor,
  type OrganizationTrendComparison,
  type OrganizationTrendDays,
  type OrganizationTrendFigure,
  type OrganizationTrendSpan,
  type OrganizationTrends,
  type OrganizationTrendsView,
} from "@/lib/organization-trends";
import type { OrganizationTrendBucketSelection, OrganizationTrendFigureId } from "@/lib/organization-trends-breakdown";
import { formatTakingsTick, type TrendsView } from "@/lib/sales-trends";
import {
  STACK_COMPARISON_ID,
  bucketSelectionIn,
  formatTrendChange,
  type TrendStackDatum,
} from "@/lib/trends-chart";

import { OrganizationTrendBreakdownPanel } from "./organization-trends-breakdown";
import { OrganizationEventTableCard } from "./organization-trends-events";

/** Home's own path: the reading's address is this plus its query. */
const HOME_PATH = "/";

/** What the span picker's controls are named by: `organization-trends-span` and the rest. */
const ID_PREFIX = "organization-trends";

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

const CHART_HEIGHT = 240;

/** The figures a chart is drawn for, by their names in the API, which a
 * bucket's breakdown is asked for by. */
const FIGURE_IDS = {
  takings: "takings",
  ticketsSold: "tickets_sold",
  pageViews: "page_views",
  eventsHeld: "events_held",
} as const satisfies Record<string, OrganizationTrendFigureId>;

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
  return (
    <TrendsSpanPicker
      scheme={ORGANIZATION_TRENDS_SPAN_SCHEME}
      view={view}
      shown={shown}
      waiting={waiting}
      onShow={onShow}
      idPrefix={ID_PREFIX}
      oneColumnOnPhone
      copy={{
        spanLabel: t("spanLabel"),
        compareLabel: t("compareLabel"),
        noComparison: t("compareNone"),
        fromLabel: t("fromLabel"),
        toLabel: t("toLabel"),
        applyRange: t("applyRange"),
        spanName: (span) => t(SPAN_KEYS[span]),
        comparisonName: (comparison) => t(COMPARISON_KEYS[comparison]),
      }}
    />
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
 *
 * Beneath the charts sits the Event table (#865): every Event of the span,
 * ranked, linking into its Sales Trends and Reach.
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
  const channelName = useSalesChannelName();
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

      <OrganizationEventTableCard trends={trends} locale={locale} />
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
  return (
    <TrendFigureCard
      title={title}
      description={description}
      breakdown={
        breakdown === undefined
          ? undefined
          : (returnFocus) => (
              <OrganizationTrendBreakdownPanel
                selection={breakdown.selection}
                figure={title}
                bucket={bucketName(breakdown.selection)}
                formatValue={breakdown.formatValue}
                onClose={() => {
                  returnFocus();
                  breakdown.onClose();
                }}
              />
            )
      }
    >
      {children}
    </TrendFigureCard>
  );
}

/**
 * What lets a Daily chart's buckets be chosen (#864): which of them is
 * selected, what a choice opens, and how each is read aloud. Choosing the
 * selected bucket again keeps it open; the panel is closed by its own button.
 */
function organizationChartSelection(
  data: readonly TrendStackDatum[],
  { figureId, selected, onSelect }: BucketChoice,
  label: string,
  describe: (datum: TrendStackDatum) => string,
): ChartBucketSelection {
  return chartSelection(
    data,
    {
      selectedKey: bucketSelectionIn(selected, figureId, data),
      onChoose: (datum) => onSelect({ figure: figureId, key: datum.key, firstDay: datum.firstDay, lastDay: datum.lastDay }),
    },
    label,
    describe,
  );
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
  const { yMax, yTicks } = useTrendYAxis(data);
  const ariaLabel = cumulative
    ? t("chartLabelCumulative", { figure: figureName, granularity: trends.granularity })
    : t("chartLabel", { figure: figureName, granularity: trends.granularity });

  return (
    <>
      {/* A figure of one series is named by its card, and has no key. */}
      {series.length > 1 ? <TrendSeriesKey label={t("channelsLabel")} series={series} /> : null}
      <TrendChartWindow
        ariaLabel={t("scrollLabel", { figure: figureName })}
        scrollAnchor={organizationTrendsScrollAnchor(trends, view)}
      >
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
              selection={organizationChartSelection(
                data,
                { figureId, selected, onSelect },
                t("selectBucket", { figure: figureName, granularity: trends.granularity }),
                describeBucketValue(t, format),
              )}
              ariaLabel={ariaLabel}
            />
          )
        }
      </TrendChartWindow>
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
  const ariaLabel = cumulative
    ? t("chartLabelCumulative", { figure: figureName, granularity: trends.granularity })
    : t("chartLabel", { figure: figureName, granularity: trends.granularity });

  return (
    <TrendLineChart
      scrollLabel={t("scrollLabel", { figure: figureName })}
      scrollAnchor={organizationTrendsScrollAnchor(trends, view)}
      height={CHART_HEIGHT}
      data={data}
      series={series}
      comparisonSeries={cumulative ? undefined : comparisonLine(trends, t, chartSeriesColor(0))}
      formatValue={format}
      // A bucket nobody loaded is a zero the hover card names.
      nothingCountedLabel={t("nothingCounted")}
      partialLabel={t("partial")}
      selection={
        cumulative
          ? undefined
          : organizationChartSelection(
              data,
              { figureId, selected, onSelect },
              t("selectBucket", { figure: figureName, granularity: trends.granularity }),
              describeBucketValue(t, format),
            )
      }
      ariaLabel={ariaLabel}
    />
  );
}
