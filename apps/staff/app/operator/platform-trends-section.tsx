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
  Input,
  Label,
  MultiSeriesLineChart,
  Skeleton,
  StackedBarChart,
  TrendTile,
  chartSeriesColor,
  type ChartBucketSelection,
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
  allBeforeMarker,
  comparisonReachesSalesOnly,
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
  type BucketLabel,
  type PlatformTrendBucket,
  type PlatformTrendComparison,
  type PlatformTrendFigure,
  type PlatformTrendSeries,
  type PlatformTrendSpan,
  type PlatformTrends,
  type PlatformTrendsView,
  type TrendChartDatum,
  type TrendStackDatum,
  type TrendTileInput,
} from "@/lib/platform-trends";
import {
  bucketSelectionIn,
  type PlatformTrendFigureId,
  type TrendBucketSelection,
} from "@/lib/platform-trends-breakdown";
import { cumulativePlotWidth, formatTakingsTick, trendsYTicks } from "@/lib/sales-trends";
import { useElementWidth } from "@/lib/use-element-width";

import { PlatformTrendBreakdownPanel } from "./platform-trends-breakdown";

/** The Overview's own path: the reading's address is this plus its query. */
const OVERVIEW_PATH = "/operator";

const SELECT_CLASS =
  "flex h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring";

const SPAN_KEYS = {
  "7d": "trendsSpan7d",
  "30d": "trendsSpan30d",
  "90d": "trendsSpan90d",
  "12m": "trendsSpan12m",
  custom: "trendsSpanCustom",
} as const satisfies Record<PlatformTrendSpan, string>;

const COMPARISON_KEYS = {
  previous: "trendsComparePrevious",
  year_earlier: "trendsCompareYearEarlier",
} as const satisfies Record<PlatformTrendComparison, string>;

/** The Sales Channels Tickets Sold is split by, by their keys in `sales`,
 * which coined the words: the operator's screen reads them and never coins a
 * second. A channel the API adds before this app names it is shown by its own
 * key. */
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
  const t = useTranslations("operator");
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
            (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ?? t("trendsLoadFailed"),
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
          {t("trendsTitle")}
        </h2>
        <p className="text-sm text-muted-foreground">
          {/* The clock the reading says it was read on, and the platform's
              own until a reading has arrived. */}
          {t("trendsDescription", { timezone: trends?.timezone ?? PLATFORM_TIME_ZONE })}
        </p>
      </div>

      <div className="grid gap-3 rounded-md border p-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="flex flex-col gap-1">
          <Label htmlFor="platform-trends-span">{t("trendsSpanLabel")}</Label>
          <select
            id="platform-trends-span"
            className={SELECT_CLASS}
            value={view.span}
            onChange={(event) =>
              show(platformTrendsViewAfterSpanChange(view, event.target.value as PlatformTrendSpan, trends?.span ?? null))
            }
          >
            {PLATFORM_TREND_SPANS.map((span) => (
              // Custom starts from the span on screen, so it waits for the
              // reading the address names to be the one on screen.
              <option
                key={span}
                value={span}
                disabled={span === "custom" && view.span !== "custom" && (trends === null || loading)}
              >
                {t(SPAN_KEYS[span])}
              </option>
            ))}
          </select>
        </div>
        {view.span === "custom" ? (
          <CustomRangeForm
            from={view.from}
            to={view.to}
            onApply={(range) => show(platformTrendsViewAfterRangeChange(view, range))}
          />
        ) : null}
        <div className="flex flex-col gap-1">
          <Label htmlFor="platform-trends-compare">{t("trendsCompareLabel")}</Label>
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
          <AlertTitle>{t("trendsLoadFailedTitle")}</AlertTitle>
          <AlertDescription>{loadError}</AlertDescription>
        </Alert>
      ) : trends === null ? (
        <div role="status" aria-label={t("trendsLoading")}>
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
  onApply,
}: {
  from: string;
  to: string;
  onApply: (range: { from: string; to: string }) => void;
}) {
  const t = useTranslations("operator");
  const [range, setRange] = useState({ from, to });
  useEffect(() => {
    setRange({ from, to });
  }, [from, to]);
  return (
    <form
      className="grid gap-3 sm:col-span-2 sm:grid-cols-[1fr_1fr_auto] sm:items-end"
      onSubmit={(event) => {
        event.preventDefault();
        onApply(range);
      }}
    >
      {/* Each day bounds the other. What else a range may be is the API's to
          refuse, and its refusal is shown below with this form still here. */}
      <div className="flex flex-col gap-1">
        <Label htmlFor="platform-trends-from">{t("trendsFromLabel")}</Label>
        <Input
          id="platform-trends-from"
          type="date"
          required
          value={range.from}
          max={range.to || undefined}
          onChange={(event) => setRange((current) => ({ ...current, from: event.target.value }))}
          className="h-9"
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="platform-trends-to">{t("trendsToLabel")}</Label>
        <Input
          id="platform-trends-to"
          type="date"
          required
          value={range.to}
          min={range.from || undefined}
          onChange={(event) => setRange((current) => ({ ...current, to: event.target.value }))}
          className="h-9"
        />
      </div>
      <Button type="submit" variant="outline" size="sm" className="h-9">
        {t("trendsApplyRange")}
      </Button>
    </form>
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
  const t = useTranslations("operator");
  const tSales = useTranslations("sales");
  const { figures } = trends;

  // The bucket whose breakdown is open (#822): one for the whole section, so
  // choosing a bucket on another chart moves the panel there. It is held with
  // the reading it was chosen on and let go when another reading arrives: a
  // new span draws other buckets, and a day chosen on the one before is not
  // left open under them.
  const [chosen, setChosen] = useState<{ trends: PlatformTrends; selection: TrendBucketSelection } | null>(null);
  const selected = chosen !== null && chosen.trends === trends ? chosen.selection : null;
  const onSelect = useCallback(
    (selection: TrendBucketSelection) => setChosen({ trends, selection }),
    [trends],
  );
  const breakdownOf = (figure: PlatformTrendFigureId): FigureBreakdown | undefined =>
    selected?.figure === figure
      ? { selection: selected, granularity: trends.granularity, locale, onClose: () => setChosen(null) }
      : undefined;

  const count = (value: number) => formatNumber(value, locale);
  const mean = (value: number) => formatTrendMean(value, locale);
  const changeCopy: TrendTileChangeCopy = {
    format: (ratio) => formatTrendChange(ratio, locale),
    growth: t("trendsChangeGrowth"),
    decline: t("trendsChangeDecline"),
    unchanged: t("trendsChangeUnchanged"),
    nothingToCompare: t("trendsNothingToCompare"),
  };
  const channelName = (key: string) => {
    const messageKey = CHANNEL_KEYS[key];
    return messageKey === undefined ? key : tSales(messageKey);
  };
  const money = (cents: number, currency: string) => formatMoney(cents, currency, locale);

  // The helpers say what each tile states, in tokens; the words are put on
  // here, where the catalog is.
  const absenceWords = { notRecorded: t("trendsNotRecorded"), noFigure: t("trendsNoFigure") };
  const secondaryLabel = (id: string) =>
    id === "distinct_7d" ? t("trendsDistinct7Days") : id === "distinct_30d" ? t("trendsDistinct30Days") : channelName(id);
  const tile = (id: string, label: string, input: TrendTileInput) => ({
    id,
    label,
    readings: input.readings.map((reading) => ({
      id: reading.id,
      value: reading.value ?? absenceWords[reading.absence ?? "noFigure"],
      caption: reading.dailyMean ? t("trendsMeanADay") : reading.currency,
      change: reading.change,
      trend: reading.trend,
    })),
    secondary: input.secondary?.map((entry) => ({ ...entry, label: secondaryLabel(entry.id) })),
  });
  const activityFormat = { mean, count };
  const tiles = [
    tile("active_customers", t("trendsActiveCustomers"), activityTile(figures.active_customers, activityFormat)),
    tile("page_views", t("trendsPageViews"), countTile(figures.page_views, count)),
    tile("active_organizations", t("trendsActiveOrganizations"), activityTile(figures.active_organizations, activityFormat)),
    tile("events_published", t("trendsEventsPublished"), countTile(figures.events_published, count)),
    tile("tickets_sold", t("trendsTicketsSold"), countTile(figures.tickets_sold, count, { splitBySeries: true })),
    tile("money_sold", t("trendsMoneySold"), moneyTile(figures.money_sold, money)),
    tile("platform_fee_earned", t("trendsPlatformFeeEarned"), moneyTile(figures.platform_fee_earned, money)),
    tile("new_verified_customers", t("trendsNewVerifiedCustomers"), countTile(figures.new_verified_customers, count)),
    tile("new_organizations", t("trendsNewOrganizations"), countTile(figures.new_organizations, count)),
  ];

  const label = useCallback((bucket: PlatformTrendBucket) => formatTrendBucket(bucket, locale), [locale]);
  const span = spanBuckets(trends);
  const comparisonName = t(COMPARISON_KEYS[trends.comparison.kind] ?? "trendsComparePrevious");
  const chartProps = (figureId: PlatformTrendFigureId): SharedChartProps => ({
    granularity: trends.granularity,
    comparisonName,
    locale,
    figureId,
    selected,
    onSelect,
  });

  const activeCustomers = activityChartData(figures.active_customers, span, label);
  const activeOrganizations = activityChartData(figures.active_organizations, span, label);
  const organizationsMarker = markerBucketKey(activeOrganizations, figures.active_organizations.exact_since);
  const customersMarker = markerBucketKey(activeCustomers, figures.active_customers.exact_since);

  return (
    <>
      <p className="text-sm text-muted-foreground">
        {t("trendsReading", {
          from: formatCalendarDay(trends.span.first_day, locale),
          to: formatCalendarDay(trends.span.last_day, locale),
          granularity: trends.granularity,
          compareFrom: formatCalendarDay(trends.comparison.first_day, locale),
          compareTo: formatCalendarDay(trends.comparison.last_day, locale),
        })}
      </p>

      <div role="group" aria-label={t("trendsTilesLabel")} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {tiles.map((entry) => (
          <TrendTile
            key={entry.id}
            label={entry.label}
            readings={entry.readings}
            secondary={entry.secondary}
            changeCopy={changeCopy}
          />
        ))}
      </div>

      <div className="space-y-1 text-xs text-muted-foreground">
        <p>{t("trendsNotFinalNote")}</p>
        <p>{t("trendsComparisonNote")}</p>
        <p>{t("trendsSelectNote")}</p>
      </div>

      <div role="group" aria-label={t("trendsChartsLabel")} className="grid gap-4 lg:grid-cols-2">
        <FigureCard
          title={t("trendsActiveCustomers")}
          description={t("trendsActiveCustomersDescription")}
          breakdown={breakdownOf("active_customers")}
        >
          <LineFigureChart
            {...chartProps("active_customers")}
            figure={t("trendsActiveCustomers")}
            data={activeCustomers}
            seriesId={ACTIVITY_SERIES_ID}
            formatValue={mean}
            marker={customersMarker === null ? undefined : { key: customersMarker, label: t("trendsMarkerRecordingBegan") }}
            // Not recorded at all is said in its own words. A recorded figure
            // can still have no frozen day in the span, which is not a zero
            // either.
            notRecordedLabel={
              figures.active_customers.recorded ? t("trendsChartNotRecorded") : t("trendsActiveCustomersNotRecorded")
            }
          />
        </FigureCard>

        <FigureCard
          title={t("trendsPageViews")}
          description={t("trendsPageViewsDescription")}
          breakdown={breakdownOf("page_views")}
        >
          <CountFigureChart
            {...chartProps("page_views")}
            figure={t("trendsPageViews")}
            source={figures.page_views}
            label={label}
          />
        </FigureCard>

        <FigureCard
          title={t("trendsActiveOrganizations")}
          description={t("trendsActiveOrganizationsDescription")}
          breakdown={breakdownOf("active_organizations")}
        >
          <LineFigureChart
            {...chartProps("active_organizations")}
            figure={t("trendsActiveOrganizations")}
            data={activeOrganizations}
            seriesId={ACTIVITY_SERIES_ID}
            formatValue={mean}
            marker={organizationsMarker === null ? undefined : { key: organizationsMarker, label: t("trendsMarkerSalesOnly") }}
            notRecordedLabel={t("trendsChartNotRecorded")}
          />
          {/* Without a marker on the chart to say it, the days shown are said
              to be sales only in words. */}
          {salesOnlyThroughout(figures.active_organizations, activeOrganizations) ? (
            <p className="text-xs text-muted-foreground">{t("trendsSalesOnlyThroughout")}</p>
          ) : comparisonReachesSalesOnly(figures.active_organizations, trends.comparison) ? (
            // The days shown are full ones and the comparison's are not all:
            // the change on the tile sets one against the other.
            <p className="text-xs text-muted-foreground">{t("trendsComparisonSalesOnly")}</p>
          ) : null}
        </FigureCard>

        <FigureCard
          title={t("trendsEventsPublished")}
          description={t("trendsEventsPublishedDescription")}
          breakdown={breakdownOf("events_published")}
        >
          <CountFigureChart
            {...chartProps("events_published")}
            figure={t("trendsEventsPublished")}
            source={figures.events_published}
            label={label}
            markerLabel={t("trendsMarkerExact")}
            beforeMarkerNote={t("trendsStandInThroughout")}
          />
        </FigureCard>

        <FigureCard
          title={t("trendsTicketsSold")}
          description={t("trendsTicketsSoldDescription")}
          breakdown={breakdownOf("tickets_sold")}
        >
          <TicketsSoldChart {...chartProps("tickets_sold")} source={figures.tickets_sold} label={label} channelName={channelName} />
        </FigureCard>

        <FigureCard
          title={t("trendsMoneySold")}
          description={t("trendsMoneySoldDescription")}
          breakdown={breakdownOf("money_sold")}
        >
          <MoneyFigureCharts
            {...chartProps("money_sold")}
            figure={t("trendsMoneySold")}
            source={figures.money_sold}
            label={label}
          />
        </FigureCard>

        <FigureCard
          title={t("trendsPlatformFeeEarned")}
          description={t("trendsPlatformFeeEarnedDescription")}
          breakdown={breakdownOf("platform_fee_earned")}
        >
          <MoneyFigureCharts
            {...chartProps("platform_fee_earned")}
            figure={t("trendsPlatformFeeEarned")}
            source={figures.platform_fee_earned}
            label={label}
          />
        </FigureCard>

        <FigureCard
          title={t("trendsNewVerifiedCustomers")}
          description={t("trendsNewVerifiedCustomersDescription")}
          breakdown={breakdownOf("new_verified_customers")}
        >
          <CountFigureChart
            {...chartProps("new_verified_customers")}
            figure={t("trendsNewVerifiedCustomers")}
            source={figures.new_verified_customers}
            label={label}
          />
        </FigureCard>

        <FigureCard
          title={t("trendsNewOrganizations")}
          description={t("trendsNewOrganizationsDescription")}
          breakdown={breakdownOf("new_organizations")}
        >
          <CountFigureChart
            {...chartProps("new_organizations")}
            figure={t("trendsNewOrganizations")}
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
  /** The figure's name in the API, which its breakdown is asked for by. */
  figureId: PlatformTrendFigureId;
  /** The bucket whose breakdown is open, on whichever chart it was chosen. */
  selected: TrendBucketSelection | null;
  onSelect: (selection: TrendBucketSelection) => void;
};

/** The open breakdown of a figure, as its card draws it beneath the chart. */
type FigureBreakdown = {
  selection: TrendBucketSelection;
  granularity: PlatformTrends["granularity"];
  locale: AppLocale;
  onClose: () => void;
};

/**
 * One figure's card: its chart, and beneath it the breakdown of the bucket
 * chosen on that chart, while one is.
 */
function FigureCard({
  title,
  description,
  breakdown,
  children,
}: {
  title: string;
  description: string;
  breakdown?: FigureBreakdown;
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
          <PlatformTrendBreakdownPanel
            selection={breakdown.selection}
            figure={title}
            granularity={breakdown.granularity}
            locale={breakdown.locale}
            onClose={() => {
              // The Close button goes with the panel. Focus returns to the
              // chart the bucket was chosen on, so a keyboard reader is not
              // dropped at the top of the page.
              content.current?.querySelector<HTMLElement>('[role="listbox"]')?.focus();
              breakdown.onClose();
            }}
          />
        )}
      </CardContent>
    </Card>
  );
}

/**
 * What lets a chart's buckets be chosen (#822): which of them is selected,
 * what a choice opens, and how each is read aloud. Choosing the selected
 * bucket again keeps it open; the panel is closed by its own button.
 */
function chartSelection<Datum extends TrendChartDatum>(
  figureId: PlatformTrendFigureId,
  data: readonly Datum[],
  { selected, onSelect }: Pick<SharedChartProps, "selected" | "onSelect">,
  label: string,
  describe: (datum: Datum) => string,
): ChartBucketSelection {
  const find = (key: string) => data.find((datum) => datum.key === key);
  return {
    selectedKey: bucketSelectionIn(selected, figureId, data),
    onSelect: (key) => {
      const datum = find(key);
      if (datum !== undefined) {
        onSelect({
          figure: figureId,
          key,
          firstDay: datum.firstDay,
          lastDay: datum.lastDay,
          partial: datum.partial === true,
        });
      }
    },
    label,
    describeBucket: (key) => {
      const datum = find(key);
      return datum === undefined ? key : describe(datum);
    },
  };
}

/**
 * The window one chart is drawn in, and the width it is drawn at: the card's
 * own, so a span is looked at whole, as the Cumulative view of Sales Trends
 * is. A card narrower than the floor scrolls sideways instead of squeezing.
 */
function ChartWindow({ figure, children }: { figure: string; children: (plotWidth: number) => ReactNode }) {
  const t = useTranslations("operator");
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotWidth = cumulativePlotWidth(availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0);
  return (
    <ChartScrollArea ref={setScrollArea} ariaLabel={t("trendsScrollLabel", { figure })}>
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
  figureId,
  currency,
  data,
  seriesId,
  formatValue,
  formatTickValue,
  marker,
  notRecordedLabel,
  granularity,
  comparisonName,
  selected,
  onSelect,
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
  const t = useTranslations("operator");
  const series: StackedSeries[] = [{ id: seriesId, name: currency ?? figure, color: chartSeriesColor(0) }];
  const comparisonSeries: StackedSeries[] = [
    { id: comparisonSeriesId(seriesId), name: comparisonName, color: chartSeriesColor(0) },
  ];
  const yMax = useMemo(() => trendChartYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  const selection = chartSelection(
    figureId,
    data,
    { selected, onSelect },
    currency === undefined
      ? t("trendsSelectBucket", { figure, granularity })
      : t("trendsSelectBucketCurrency", { figure, currency, granularity }),
    (datum) => {
      const value = datum.values[seriesId];
      if (value === null || value === undefined) {
        return t("trendsBucketNoFigure", { bucket: datum.label });
      }
      return t(datum.partial ? "trendsBucketPartial" : "trendsBucketReading", {
        bucket: datum.label,
        value: formatValue(value),
      });
    },
  );
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
          nothingCountedLabel={t("trendsNothingCounted")}
          partialLabel={t("trendsPartialToday")}
          marker={marker}
          selection={selection}
          notRecordedLabel={notRecordedLabel}
          ariaLabel={
            currency === undefined
              ? t("trendsChartLabel", { figure, granularity })
              : t("trendsChartLabelCurrency", { figure, currency, granularity })
          }
        />
      )}
    </ChartWindow>
  );
}

/**
 * What a figure's card says in place of its chart while the backend says the
 * figure is not recorded at all: its series would draw as zeros, and "not
 * recorded" is not a zero.
 */
function FigureNotRecorded() {
  const t = useTranslations("operator");
  return <p className="text-sm text-muted-foreground">{t("trendsChartNotRecorded")}</p>;
}

/** A figure that is one count in one series. */
function CountFigureChart({
  source,
  label,
  markerLabel,
  beforeMarkerNote,
  ...rest
}: SharedChartProps & {
  figure: string;
  source: PlatformTrendFigure;
  label: BucketLabel;
  /** What the recording-began marker is called, for a figure that has one. */
  markerLabel?: string;
  /** Said under the chart when every bucket shown is before that marker, so
   * the chart has no mark to say it. */
  beforeMarkerNote?: string;
}) {
  const data = useMemo(() => seriesChartData(source.series.slice(0, 1), label), [source, label]);
  if (!source.recorded) {
    return <FigureNotRecorded />;
  }
  const seriesId = source.series[0]?.key ?? ACTIVITY_SERIES_ID;
  const markerKey = markerLabel === undefined ? null : markerBucketKey(data, source.exact_since);
  return (
    <>
      <LineFigureChart
        {...rest}
        data={data}
        seriesId={seriesId}
        formatValue={(value) => formatNumber(value, rest.locale)}
        marker={markerKey === null || markerLabel === undefined ? undefined : { key: markerKey, label: markerLabel }}
      />
      {beforeMarkerNote !== undefined && allBeforeMarker(source.exact_since, data) ? (
        <p className="text-xs text-muted-foreground">{beforeMarkerNote}</p>
      ) : null}
    </>
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
}: SharedChartProps & {
  figure: string;
  source: PlatformTrendFigure;
  label: BucketLabel;
}) {
  const t = useTranslations("operator");
  if (!source.recorded) {
    return <FigureNotRecorded />;
  }
  if (source.series.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("trendsNoFigure")}</p>;
  }
  return (
    <>
      {source.series.map((entry) => (
        <div key={entry.key} className="space-y-1">
          {source.series.length > 1 ? <p className="text-xs font-medium text-muted-foreground">{entry.key}</p> : null}
          <MoneyCurrencyChart {...rest} figure={figure} series={entry} label={label} />
        </div>
      ))}
    </>
  );
}

/** One currency of a money figure, every amount in that currency's own units. */
function MoneyCurrencyChart({
  series,
  label,
  ...rest
}: SharedChartProps & {
  figure: string;
  series: PlatformTrendSeries;
  label: BucketLabel;
}) {
  const data = useMemo(() => seriesChartData([series], label), [series, label]);
  return (
    <LineFigureChart
      {...rest}
      currency={series.key}
      data={data}
      seriesId={series.key}
      formatValue={(cents) => formatMoney(cents, series.key, rest.locale)}
      formatTickValue={(cents) => formatTakingsTick(cents, series.key, rest.locale)}
    />
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
  figureId,
  selected,
  onSelect,
}: SharedChartProps & { source: PlatformTrendFigure; label: BucketLabel; channelName: (key: string) => string }) {
  const t = useTranslations("operator");
  const figure = t("trendsTicketsSold");
  const data: TrendStackDatum[] = useMemo(() => stackedChartData(source.series, label), [source, label]);
  const series: StackedSeries[] = source.series.map((entry, index) => ({
    id: entry.key,
    name: channelName(entry.key),
    color: chartSeriesColor(index),
  }));
  const yMax = useMemo(() => trendChartYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  if (!source.recorded) {
    return <FigureNotRecorded />;
  }
  const selection = chartSelection(
    figureId,
    data,
    { selected, onSelect },
    t("trendsSelectBucket", { figure, granularity }),
    // A stack is read by its height: the bucket's tickets on every Sales
    // Channel, which is what its breakdown adds up to.
    (datum) =>
      t(datum.partial ? "trendsBucketPartial" : "trendsBucketReading", {
        bucket: datum.label,
        value: formatNumber(datum.total, locale),
      }),
  );
  return (
    <>
      {/* A plain key, not the chips of Sales Trends: nothing here is toggled. */}
      <ul aria-label={t("trendsChannelsLabel")} className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
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
            totalLabel={t("trendsTicketsTotal")}
            partialLabel={t("trendsPartialToday")}
            selection={selection}
            ariaLabel={t("trendsChartLabel", { figure, granularity })}
          />
        )}
      </ChartWindow>
    </>
  );
}
