"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import { toAppLocale, type AppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
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
  Skeleton,
  StackedBarChart,
  TrendTile,
  chartSeriesColor,
  type ChartMarker,
  type StackedSeries,
  type TrendTileChangeCopy,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatCalendarDay, formatMoney, formatNumber } from "@/lib/format";
import {
  ACTIVITY_SERIES_ID,
  PLATFORM_TREND_COMPARISONS,
  PLATFORM_TREND_SPANS,
  STACK_COMPARISON_ID,
  activityChartData,
  activityTile,
  comparisonSeriesId,
  countTile,
  fetchPlatformTrends,
  formatTrendBucket,
  formatTrendChange,
  formatTrendMean,
  markerBucketKey,
  moneyTile,
  platformTrendsQuery,
  platformTrendsViewAfterComparisonChange,
  platformTrendsViewAfterRangeChange,
  platformTrendsViewAfterSpanChange,
  salesOnlyThroughout,
  seriesChartData,
  spanBuckets,
  stackedChartData,
  trendChartYMax,
  type PlatformTrendBucket,
  type PlatformTrendComparison,
  type PlatformTrendFigure,
  type PlatformTrendSpan,
  type PlatformTrends,
  type PlatformTrendsView,
  type TrendChartDatum,
  type TrendStackDatum,
  type TrendTileCopy,
} from "@/lib/platform-trends";
import { cumulativePlotWidth, formatTakingsTick, trendsYTicks } from "@/lib/sales-trends";
import { useElementWidth } from "@/lib/use-element-width";

/** The Overview's own path: the reading's address is this plus its query. */
const OVERVIEW_PATH = "/operator";

const SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring";

const SPAN_KEYS = {
  "7d": "span7d",
  "30d": "span30d",
  "90d": "span90d",
  "12m": "span12m",
  custom: "spanCustom",
} as const satisfies Record<PlatformTrendSpan, string>;

const COMPARISON_KEYS = {
  previous: "comparePrevious",
  year_earlier: "compareYearEarlier",
} as const satisfies Record<PlatformTrendComparison, string>;

/** The Sales Channels Tickets Sold is split by, by catalog key. A channel the
 * API adds before this app names it is shown by its own key. */
const CHANNEL_KEYS: Record<string, "channelOnline" | "channelInPerson" | "channelImport"> = {
  online: "channelOnline",
  in_person: "channelInPerson",
  import: "channelImport",
};

/** The colour of the line a stack is compared with. The stack has one colour
 * per Sales Channel and the line stands for all of them, so it takes none of
 * theirs: the text colour, which follows the theme. */
const STACK_COMPARISON_COLOR = "var(--foreground)";

/** How tall each chart is drawn. Shorter than a chart that has a card to
 * itself: there are nine, two to a row, above the rest of the Overview. */
const CHART_HEIGHT = 220;

/**
 * Platform Trends (#821, spec #808, ADR 0080): the growth reading of the whole
 * platform, at the top of the Operator Dashboard's Overview.
 *
 * The reading on screen is named by the page's address (`view`, parsed from it
 * by the page), and every choice here is a push of a new address, so a reload
 * or a shared link shows the same reading. The section asks the backend for
 * that reading in ONE request and draws what it is told: the buckets, the
 * totals, the change, the partial today and the day recording began are all
 * the backend's.
 *
 * It reads on its own, apart from the Overview's batch, and a failure is said
 * inside the section: the queues and totals beneath do not depend on it, and
 * a span the API refuses (a custom range the wrong way round) must leave the
 * controls on screen to put it right with.
 */
export function PlatformTrendsSection({ view }: { view: PlatformTrendsView }) {
  const t = useTranslations("platformTrends");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const router = useRouter();
  const [trends, setTrends] = useState<PlatformTrends | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);

  // The query is the whole fetch key: it encodes the span, its days and the
  // comparison, and is what the address bar shows.
  const query = platformTrendsQuery(view);
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    fetchPlatformTrends(view)
      .then((data) => {
        if (!cancelled) {
          setTrends(data);
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          // The reading that failed is not the one on screen any more, so the
          // old one is not left standing under a new address.
          setTrends(null);
          setLoadError(
            (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ?? t("loadFailed"),
          );
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);

  // Staying where the reader is: the controls are at the top of the page and
  // a new reading is drawn in place.
  const show = (next: PlatformTrendsView) => {
    router.push(`${OVERVIEW_PATH}${platformTrendsQuery(next)}`, { scroll: false });
  };

  return (
    <section aria-labelledby="platform-trends-title" className="space-y-4">
      <div className="space-y-1">
        <h2 id="platform-trends-title" className="text-lg font-semibold tracking-tight">
          {t("title")}
        </h2>
        <p className="text-sm text-muted-foreground">
          {/* The clock the reading says it was read on, and the platform's
              own until a reading has arrived. */}
          {t("description", { timezone: trends?.timezone ?? PLATFORM_TIME_ZONE })}
        </p>
      </div>

      <div className="grid gap-3 rounded-md border p-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="flex flex-col gap-1">
          <Label htmlFor="platform-trends-span">{t("spanLabel")}</Label>
          <select
            id="platform-trends-span"
            className={SELECT_CLASS}
            value={view.span}
            onChange={(event) =>
              show(platformTrendsViewAfterSpanChange(view, event.target.value as PlatformTrendSpan, trends?.span ?? null))
            }
          >
            {PLATFORM_TREND_SPANS.map((span) => (
              // Custom starts from the span on screen, so it waits for one.
              <option key={span} value={span} disabled={span === "custom" && trends === null && view.span !== "custom"}>
                {t(SPAN_KEYS[span])}
              </option>
            ))}
          </select>
        </div>
        {view.span === "custom" ? (
          <>
            {/* Applied on change, as the Tax Invoices list applies its dates:
                a date input commits a whole day at once. Each bounds the
                other; what else a range may be is the API's to refuse. */}
            <div className="flex flex-col gap-1">
              <Label htmlFor="platform-trends-from">{t("fromLabel")}</Label>
              <Input
                id="platform-trends-from"
                type="date"
                value={view.from}
                max={view.to}
                onChange={(event) => show(platformTrendsViewAfterRangeChange(view, { from: event.target.value }))}
                className="h-9"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="platform-trends-to">{t("toLabel")}</Label>
              <Input
                id="platform-trends-to"
                type="date"
                value={view.to}
                min={view.from}
                onChange={(event) => show(platformTrendsViewAfterRangeChange(view, { to: event.target.value }))}
                className="h-9"
              />
            </div>
          </>
        ) : null}
        <div className="flex flex-col gap-1">
          <Label htmlFor="platform-trends-compare">{t("compareLabel")}</Label>
          <select
            id="platform-trends-compare"
            className={SELECT_CLASS}
            value={view.compare}
            onChange={(event) =>
              show(platformTrendsViewAfterComparisonChange(view, event.target.value as PlatformTrendComparison))
            }
          >
            {PLATFORM_TREND_COMPARISONS.map((comparison) => (
              <option key={comparison} value={comparison}>
                {t(COMPARISON_KEYS[comparison])}
              </option>
            ))}
          </select>
        </div>
      </div>

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
        // The reading before stays on screen, dimmed, while the next loads:
        // a page of charts collapsing to a placeholder on every choice would
        // throw the reader's place away.
        <div aria-busy={loading} className={loading ? "space-y-4 opacity-60" : "space-y-4"}>
          <PlatformTrendsReading trends={trends} locale={locale} />
        </div>
      )}
    </section>
  );
}

/**
 * One reading, drawn: the tiles, then a chart per figure.
 *
 * Active Customers and Page Views are neighbours in both, since one is read
 * against the other, and are never put in one tile, one chart or one ratio:
 * the first counts people and the second counts loads.
 */
function PlatformTrendsReading({ trends, locale }: { trends: PlatformTrends; locale: AppLocale }) {
  const t = useTranslations("platformTrends");
  const { figures } = trends;

  const count = (value: number) => formatNumber(value, locale);
  const mean = (value: number) => formatTrendMean(value, locale);
  const tileCopy: TrendTileCopy = { notRecorded: t("notRecorded"), noFigure: t("noFigure") };
  const changeCopy: TrendTileChangeCopy = {
    format: (ratio) => formatTrendChange(ratio, locale),
    growth: t("changeGrowth"),
    decline: t("changeDecline"),
    unchanged: t("changeUnchanged"),
    nothingToCompare: t("nothingToCompare"),
  };
  const activityLabels = {
    mean: t("meanADay"),
    distinct7Days: t("distinct7Days"),
    distinct30Days: t("distinct30Days"),
  };
  const channelName = (key: string) => {
    const messageKey = CHANNEL_KEYS[key];
    return messageKey === undefined ? key : t(messageKey);
  };
  const money = (cents: number, currency: string) => formatMoney(cents, currency, locale);

  const tiles = [
    { id: "active_customers", label: t("activeCustomers"), ...activityTile(figures.active_customers, { mean, count }, tileCopy, activityLabels) },
    { id: "page_views", label: t("pageViews"), ...countTile(figures.page_views, count, tileCopy) },
    { id: "active_organizations", label: t("activeOrganizations"), ...activityTile(figures.active_organizations, { mean, count }, tileCopy, activityLabels) },
    { id: "events_published", label: t("eventsPublished"), ...countTile(figures.events_published, count, tileCopy) },
    { id: "tickets_sold", label: t("ticketsSold"), ...countTile(figures.tickets_sold, count, tileCopy, channelName) },
    { id: "money_sold", label: t("moneySold"), ...moneyTile(figures.money_sold, money, tileCopy) },
    { id: "platform_fee_earned", label: t("platformFeeEarned"), ...moneyTile(figures.platform_fee_earned, money, tileCopy) },
    { id: "new_verified_customers", label: t("newVerifiedCustomers"), ...countTile(figures.new_verified_customers, count, tileCopy) },
    { id: "new_organizations", label: t("newOrganizations"), ...countTile(figures.new_organizations, count, tileCopy) },
  ];

  const label = useCallback((bucket: PlatformTrendBucket) => formatTrendBucket(bucket, locale), [locale]);
  const span = spanBuckets(trends);
  const comparisonName = t(COMPARISON_KEYS[trends.comparison.kind] ?? "comparePrevious");
  const shared: SharedChartProps = {
    granularity: trends.granularity,
    comparisonName,
    locale,
  };

  const activeCustomers = activityChartData(figures.active_customers, span, label);
  const activeOrganizations = activityChartData(figures.active_organizations, span, label);
  const organizationsMarker = markerBucketKey(activeOrganizations, figures.active_organizations.exact_since);
  const customersMarker = markerBucketKey(activeCustomers, figures.active_customers.exact_since);

  return (
    <>
      <p className="text-sm text-muted-foreground">
        {t("reading", {
          from: formatCalendarDay(trends.span.first_day, locale),
          to: formatCalendarDay(trends.span.last_day, locale),
          granularity: trends.granularity,
          compareFrom: formatCalendarDay(trends.comparison.first_day, locale),
          compareTo: formatCalendarDay(trends.comparison.last_day, locale),
        })}
      </p>

      <div role="group" aria-label={t("tilesLabel")} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {tiles.map((tile) => (
          <TrendTile
            key={tile.id}
            label={tile.label}
            readings={tile.readings}
            secondary={tile.secondary}
            changeCopy={changeCopy}
          />
        ))}
      </div>

      <div className="space-y-1 text-xs text-muted-foreground">
        <p>{t("notFinalNote")}</p>
        <p>{t("comparisonNote")}</p>
      </div>

      <div role="group" aria-label={t("chartsLabel")} className="grid gap-4 lg:grid-cols-2">
        <FigureCard title={t("activeCustomers")} description={t("activeCustomersDescription")}>
          <LineFigureChart
            {...shared}
            figure={t("activeCustomers")}
            data={activeCustomers}
            seriesId={ACTIVITY_SERIES_ID}
            formatValue={mean}
            marker={customersMarker === null ? undefined : { key: customersMarker, label: t("markerRecordingBegan") }}
            // Not recorded at all is said in its own words. A recorded figure
            // can still have no frozen day in the span, which is not a zero
            // either.
            notRecordedLabel={
              figures.active_customers.recorded ? t("chartNotRecorded") : t("activeCustomersNotRecorded")
            }
          />
        </FigureCard>

        <FigureCard title={t("pageViews")} description={t("pageViewsDescription")}>
          <CountFigureChart {...shared} figure={t("pageViews")} source={figures.page_views} label={label} />
        </FigureCard>

        <FigureCard title={t("activeOrganizations")} description={t("activeOrganizationsDescription")}>
          <LineFigureChart
            {...shared}
            figure={t("activeOrganizations")}
            data={activeOrganizations}
            seriesId={ACTIVITY_SERIES_ID}
            formatValue={mean}
            marker={organizationsMarker === null ? undefined : { key: organizationsMarker, label: t("markerSalesOnly") }}
            notRecordedLabel={t("chartNotRecorded")}
          />
          {/* Without a marker on the chart to say it, the days shown are said
              to be sales only in words. */}
          {salesOnlyThroughout(figures.active_organizations, activeOrganizations) ? (
            <p className="text-xs text-muted-foreground">{t("salesOnlyThroughout")}</p>
          ) : null}
        </FigureCard>

        <FigureCard title={t("eventsPublished")} description={t("eventsPublishedDescription")}>
          <CountFigureChart
            {...shared}
            figure={t("eventsPublished")}
            source={figures.events_published}
            label={label}
            markerLabel={t("markerExact")}
          />
        </FigureCard>

        <FigureCard title={t("ticketsSold")} description={t("ticketsSoldDescription")}>
          <TicketsSoldChart {...shared} source={figures.tickets_sold} label={label} channelName={channelName} />
        </FigureCard>

        <FigureCard title={t("moneySold")} description={t("moneySoldDescription")}>
          <MoneyFigureCharts {...shared} figure={t("moneySold")} source={figures.money_sold} label={label} />
        </FigureCard>

        <FigureCard title={t("platformFeeEarned")} description={t("platformFeeEarnedDescription")}>
          <MoneyFigureCharts
            {...shared}
            figure={t("platformFeeEarned")}
            source={figures.platform_fee_earned}
            label={label}
          />
        </FigureCard>

        <FigureCard title={t("newVerifiedCustomers")} description={t("newVerifiedCustomersDescription")}>
          <CountFigureChart
            {...shared}
            figure={t("newVerifiedCustomers")}
            source={figures.new_verified_customers}
            label={label}
          />
        </FigureCard>

        <FigureCard title={t("newOrganizations")} description={t("newOrganizationsDescription")}>
          <CountFigureChart
            {...shared}
            figure={t("newOrganizations")}
            source={figures.new_organizations}
            label={label}
          />
        </FigureCard>
      </div>
    </>
  );
}

/** What every chart of a reading is told alike. */
type SharedChartProps = {
  granularity: PlatformTrends["granularity"];
  /** What the comparison line is called in a hover card. */
  comparisonName: string;
  locale: AppLocale;
};

type BucketLabel = (bucket: PlatformTrendBucket) => string;

function FigureCard({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">{children}</CardContent>
    </Card>
  );
}

/**
 * The window one chart is drawn in, and the width it is drawn at: the card's
 * own, so a span is looked at whole, as the Cumulative view of Sales Trends
 * is. A card narrower than the floor scrolls sideways instead of squeezing.
 */
function ChartWindow({ figure, children }: { figure: string; children: (plotWidth: number) => ReactNode }) {
  const t = useTranslations("platformTrends");
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotWidth = cumulativePlotWidth(availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0);
  return (
    <ChartScrollArea ref={setScrollArea} ariaLabel={t("scrollLabel", { figure })}>
      {children(plotWidth)}
    </ChartScrollArea>
  );
}

/**
 * One figure drawn as one line, with the comparison span as a faint line
 * beside it, today (where the data carries it) as the partial stretch, and
 * the recording-began marker where the caller places one.
 */
function LineFigureChart({
  figure,
  currency,
  data,
  seriesId,
  formatValue,
  formatTickValue,
  marker,
  notRecordedLabel,
  granularity,
  comparisonName,
}: SharedChartProps & {
  /** The figure's name, for the chart's accessible name and hover card. */
  figure: string;
  /** Named in the accessible name when the figure is drawn once per currency. */
  currency?: string;
  data: TrendChartDatum[];
  seriesId: string;
  formatValue: (value: number) => string;
  formatTickValue?: (value: number) => string;
  marker?: ChartMarker;
  notRecordedLabel?: string;
}) {
  const t = useTranslations("platformTrends");
  const series: StackedSeries[] = [{ id: seriesId, name: currency ?? figure, color: chartSeriesColor(0) }];
  const comparisonSeries: StackedSeries[] = [
    { id: comparisonSeriesId(seriesId), name: comparisonName, color: chartSeriesColor(0) },
  ];
  const yMax = useMemo(() => trendChartYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  return (
    <ChartWindow figure={figure}>
      {(plotWidth) => (
        <MultiSeriesLineChart
          data={data}
          series={series}
          comparisonSeries={comparisonSeries}
          yMax={yMax}
          yTicks={yTicks}
          plotWidth={plotWidth}
          height={CHART_HEIGHT}
          formatValue={formatValue}
          formatTickValue={formatTickValue}
          // A day nothing happened on is a finding here, and is named as a
          // zero beside a comparison that may not be one.
          zeroIsMeasured
          nothingCountedLabel={t("nothingCounted")}
          partialLabel={t("partialToday")}
          marker={marker}
          notRecordedLabel={notRecordedLabel}
          ariaLabel={
            currency === undefined
              ? t("chartLabel", { figure, granularity })
              : t("chartLabelCurrency", { figure, currency, granularity })
          }
        />
      )}
    </ChartWindow>
  );
}

/** A figure that is one count in one series. */
function CountFigureChart({
  source,
  label,
  markerLabel,
  ...rest
}: SharedChartProps & { figure: string; source: PlatformTrendFigure; label: BucketLabel; markerLabel?: string }) {
  const data = useMemo(() => seriesChartData(source.series.slice(0, 1), label), [source, label]);
  const seriesId = source.series[0]?.key ?? ACTIVITY_SERIES_ID;
  const markerKey = markerLabel === undefined ? null : markerBucketKey(data, source.exact_since);
  return (
    <LineFigureChart
      {...rest}
      data={data}
      seriesId={seriesId}
      formatValue={(value) => formatNumber(value, rest.locale)}
      marker={markerKey === null || markerLabel === undefined ? undefined : { key: markerKey, label: markerLabel }}
    />
  );
}

/**
 * A money figure: one chart per currency, each on its own axis. Two
 * currencies on one axis would set amounts beside each other that nothing on
 * this platform converts, so they are never drawn together.
 */
function MoneyFigureCharts({
  source,
  label,
  figure,
  ...rest
}: SharedChartProps & { figure: string; source: PlatformTrendFigure; label: BucketLabel }) {
  const t = useTranslations("platformTrends");
  if (source.series.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("noFigure")}</p>;
  }
  return (
    <>
      {source.series.map((entry) => (
        <div key={entry.key} className="space-y-1">
          {source.series.length > 1 ? <p className="text-xs font-medium text-muted-foreground">{entry.key}</p> : null}
          <LineFigureChart
            {...rest}
            figure={figure}
            currency={entry.key}
            data={seriesChartData([entry], label)}
            seriesId={entry.key}
            formatValue={(cents) => formatMoney(cents, entry.key, rest.locale)}
            formatTickValue={(cents) => formatTakingsTick(cents, entry.key, rest.locale)}
          />
        </div>
      ))}
    </>
  );
}

/**
 * Tickets Sold, one bar per bucket stacked by Sales Channel, with the height
 * of the comparison span's stack as the faint line and today as a faint bar.
 */
function TicketsSoldChart({
  source,
  label,
  channelName,
  granularity,
  comparisonName,
  locale,
}: SharedChartProps & { source: PlatformTrendFigure; label: BucketLabel; channelName: (key: string) => string }) {
  const t = useTranslations("platformTrends");
  const figure = t("ticketsSold");
  const data: TrendStackDatum[] = useMemo(() => stackedChartData(source.series, label), [source, label]);
  const series: StackedSeries[] = source.series.map((entry, index) => ({
    id: entry.key,
    name: channelName(entry.key),
    color: chartSeriesColor(index),
  }));
  const yMax = useMemo(() => trendChartYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  return (
    <>
      {/* A plain key, not the chips of Sales Trends: nothing here is toggled. */}
      <ul aria-label={t("channelsLabel")} className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {series.map((entry) => (
          <li key={entry.id} className="flex items-center gap-1.5">
            <span aria-hidden className="size-2.5 rounded-sm" style={{ backgroundColor: entry.color }} />
            {entry.name}
          </li>
        ))}
      </ul>
      <ChartWindow figure={figure}>
        {(plotWidth) => (
          <StackedBarChart
            data={data}
            series={series}
            comparisonSeries={[{ id: STACK_COMPARISON_ID, name: comparisonName, color: STACK_COMPARISON_COLOR }]}
            yMax={yMax}
            yTicks={yTicks}
            plotWidth={plotWidth}
            height={CHART_HEIGHT}
            formatValue={(value) => formatNumber(value, locale)}
            totalLabel={t("ticketsTotal")}
            partialLabel={t("partialToday")}
            ariaLabel={t("chartLabel", { figure, granularity })}
          />
        )}
      </ChartWindow>
    </>
  );
}
