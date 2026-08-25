"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  CHART_MARGIN,
  CHART_PLOT_INSET,
  ChartLegendChips,
  ChartScrollArea,
  ChartViewToggle,
  MultiSeriesLineChart,
  Skeleton,
  Y_AXIS_WIDTH,
  chartSeriesColor,
  type ChartLegendChip,
  type ChartViewOption,
  type StackedSeries,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import {
  AFFILIATE_TRENDS_GRANULARITIES,
  AFFILIATE_TRENDS_RANGES,
  ALL_PAGE_VIEWS_ID,
  DEFAULT_AFFILIATE_RATE_VIEW,
  DEFAULT_AFFILIATE_TRENDS_RANGE,
  RATE_TRENDS_RANGES,
  affiliateTrendsPlotWidth,
  availableTrendsMetrics,
  countYTicks,
  fetchAffiliateTrends,
  granularityChoosable,
  hasTrendsData,
  latestBucketsScrollLeft,
  listTrendsSeries,
  multiSeriesYMax,
  rangeGranularity,
  rateRange,
  rateSeries,
  rateYMax,
  rateYTicks,
  salesSeries,
  toggleListedSeries,
  trendsGranularity,
  viewsSeries,
  type AffiliateRateDatum,
  type AffiliateRateView,
  type AffiliateSalesDatum,
  type AffiliateTrends,
  type AffiliateTrendsGranularity,
  type AffiliateTrendsMetric,
  type AffiliateTrendsRange,
} from "@/lib/affiliate-trends";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatMoney, formatNumber, formatPercent } from "@/lib/format";
import { cumulativePlotWidth } from "@/lib/sales-trends";

type AffiliateTrendsSectionProps = {
  eventId: string;
};

/**
 * The Affiliate Link trends surface: each link's Clicks, hour by hour, set
 * against everything the Event page received (ADR 0057, #411).
 *
 * The whole bounded history arrives in one request and never comes back to the
 * network: range switching and legend toggling are pure transforms of the
 * matrix in hand, exactly as Sales Trends works. The state — which series are
 * drawn, and how far back the chart looks — lives here above the chart, because
 * one legend and one range picker must drive everything the surface ever draws.
 *
 * Every figure is a floor, not a measurement (ADR 0022): buckets count page
 * loads, not people, and the copy around the chart says so. Events with
 * External Registration get this view too — clicks and page views are the only
 * signal their links give.
 */
export function AffiliateTrendsSection({ eventId }: AffiliateTrendsSectionProps) {
  const t = useTranslations("affiliateTrends");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [trends, setTrends] = useState<AffiliateTrends | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  // Which series are drawn. Everything starts selected — the first look is the
  // whole page against every link, and narrowing is the reader's move.
  const [selected, setSelected] = useState<string[]>([]);
  // How far back the chart looks. Local, not in the URL, like Sales Trends'
  // view: every visit opens on the default week.
  const [range, setRange] = useState<AffiliateTrendsRange>(DEFAULT_AFFILIATE_TRENDS_RANGE);
  // How finely the counting views are drawn. Every range opens at its own
  // default (hourly for the short ones, daily for the long) and the reader
  // switches from there; a range switch resets it so a month never opens as
  // seven hundred hourly slivers because the week before was read by the hour.
  const [chosenGranularity, setChosenGranularity] = useState<AffiliateTrendsGranularity>(
    rangeGranularity(DEFAULT_AFFILIATE_TRENDS_RANGE),
  );
  const granularity = trendsGranularity(range, chosenGranularity);
  const onRangeChange = useCallback((next: AffiliateTrendsRange) => {
    setRange(next);
    setChosenGranularity(rangeGranularity(next));
  }, []);
  // Which measure the one chart draws. Clicks first — how the page was reached
  // is the surface's first question, and the only one an externally registered Event can answer.
  const [metric, setMetric] = useState<AffiliateTrendsMetric>("clicks");
  // How the Rate view counts. Cumulative by default: the seven-day Attribution
  // Window makes a day's sales answer an earlier day's clicks, and the running
  // division is the reading that absorbs that lag (ADR 0057).
  const [rateView, setRateView] = useState<AffiliateRateView>(DEFAULT_AFFILIATE_RATE_VIEW);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    fetchAffiliateTrends(eventId)
      .then((data) => {
        if (cancelled) {
          return;
        }
        setTrends(data);
        setSelected([ALL_PAGE_VIEWS_ID, ...data.links.map((link) => link.id)]);
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setLoadError(
            (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ??
              t("loadFailed"),
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
  }, [eventId]);

  // "Now" is pinned per payload rather than read per render, so the axis is a
  // statement about the data's moment and a re-render cannot shift the window
  // an hour while the reader is looking at it.
  const now = useMemo(() => new Date(), [trends]);

  // The drawable order: the whole page first, then the links as the API sends
  // them (newest first, deactivated included, so the legend never changes
  // shape). Colour is keyed on this order, so a link keeps its colour whichever
  // chips are off and whenever the chart is opened.
  const order = useMemo(
    () => [ALL_PAGE_VIEWS_ID, ...(trends?.links.map((link) => link.id) ?? [])],
    [trends],
  );
  // The whole-page chip is one series wearing two names: counting loads it is
  // "All page views"; on the Rate view it is the page's own rate — every
  // attributed sale over every load.
  const nameOf = useCallback(
    (id: string) =>
      id === ALL_PAGE_VIEWS_ID
        ? metric === "rate"
          ? t("overallRate")
          : t("allPageViews")
        : (trends?.links.find((link) => link.id === id)?.name ?? id),
    [trends, t, metric],
  );
  const colorOf = useCallback((id: string) => chartSeriesColor(order.indexOf(id)), [order]);

  // What this window and metric list: the series with something to say in the
  // span, busiest first, the whole page ahead (#431). Chips, lines and the
  // series the chart is handed all read from this one listing, so a link with
  // nothing in the window is neither a chip nor a flat zero line — and is back,
  // with its selection as the reader left it, on a range that lists it. The
  // sales view never lists the whole page: it is a views concept with no sales
  // to its name. The selection itself is shared across views and ranges.
  const listing = useMemo(
    () =>
      trends
        ? listTrendsSeries(trends, selected, metric, range, granularity, rateView, now)
        : { listed: [], drawn: [], empty: true },
    [trends, selected, metric, range, granularity, rateView, now],
  );

  const chips: ChartLegendChip[] = useMemo(
    () => listing.listed.map((id) => ({ id, label: nameOf(id), color: colorOf(id) })),
    [listing, nameOf, colorOf],
  );

  // The Rate view's range ladder starts at the week: a rate is never hourly
  // (ADR 0057), and a "24 hours" of daily points is one point. The chosen range
  // itself is shared — a reader on 24h sees the week while on the Rate view and
  // has their day back on return.
  const rangeOptions: ChartViewOption<AffiliateTrendsRange>[] = useMemo(
    () =>
      (metric === "rate" ? RATE_TRENDS_RANGES : AFFILIATE_TRENDS_RANGES).map((id) => ({
        id,
        label: t(`range_${id}`),
      })),
    [metric, t],
  );

  // Hourly or Daily, the counting views' toggle. Not on the Rate view, which
  // is never hourly (ADR 0057), and not on 24h, where a day is one point.
  const granularityOptions: ChartViewOption<AffiliateTrendsGranularity>[] = useMemo(
    () => AFFILIATE_TRENDS_GRANULARITIES.map((id) => ({ id, label: t(`granularity_${id}`) })),
    [t],
  );

  // Daily or Cumulative, the Rate view's own toggle — Sales Trends' pair of
  // countings, worded with the same words.
  const rateViewOptions: ChartViewOption<AffiliateRateView>[] = useMemo(
    () => [
      { id: "daily", label: t("rateViewDaily") },
      { id: "cumulative", label: t("rateViewCumulative") },
    ],
    [t],
  );

  // The switcher only exists when there is something to switch: an Event that
  // registers externally has no sales figures (they arrive null, not zero), so
  // it keeps the Clicks view with no control at all (#415).
  const metrics = useMemo<AffiliateTrendsMetric[]>(
    () => (trends ? availableTrendsMetrics(trends) : ["clicks"]),
    [trends],
  );
  const metricOptions: ChartViewOption<AffiliateTrendsMetric>[] = useMemo(
    () => metrics.map((id) => ({ id, label: t(`metric_${id}`) })),
    [metrics, t],
  );

  const series: StackedSeries[] = useMemo(
    () => listing.drawn.map((id) => ({ id, name: nameOf(id), color: colorOf(id) })),
    [listing, nameOf, colorOf],
  );

  // The words on the legend's Show-all button, stated here because the UI
  // package has no i18n and the count's grammar belongs to the locale.
  const collapseLabels = useMemo(
    () => ({
      showAll: (count: number) => t("showAllChips", { count }),
      showFewer: t("showFewerChips"),
    }),
    [t],
  );

  // The last chip THIS window lists cannot be deselected: a still-selected
  // series the window does not list is not on the chart, so it must not count
  // as "something is still drawn".
  const onToggle = useCallback(
    (id: string) => {
      setSelected((current) => toggleListedSeries(order, listing.listed, current, id));
    },
    [order, listing],
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("title")}</CardTitle>
        <CardDescription>{t("description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <Skeleton className="h-80 w-full" />
        ) : loadError ? (
          <Alert variant="destructive">
            <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
            <AlertDescription>{loadError}</AlertDescription>
          </Alert>
        ) : !trends ? null : hasTrendsData(trends) ? (
          <>
            <div className="flex flex-wrap items-center justify-between gap-3">
              {/* Collapsed to three rows on a busy Event (#433): the chart is the
                  point, and thirty chips would push it below the fold. Sales
                  Trends' legend is not collapsed — a Ticket Type is never that
                  numerous, and its chips are the reading. */}
              <ChartLegendChips
                chips={chips}
                selected={listing.drawn}
                onToggle={onToggle}
                ariaLabel={t("chipsLabel")}
                collapsible={collapseLabels}
              />
              <div className="flex flex-wrap items-center gap-2">
                {metricOptions.length > 1 ? (
                  <ChartViewToggle
                    options={metricOptions}
                    value={metric}
                    onChange={setMetric}
                    ariaLabel={t("metricLabel")}
                  />
                ) : null}
                {metric === "rate" ? (
                  <ChartViewToggle
                    options={rateViewOptions}
                    value={rateView}
                    onChange={setRateView}
                    ariaLabel={t("rateViewLabel")}
                  />
                ) : granularityChoosable(range) ? (
                  <ChartViewToggle
                    options={granularityOptions}
                    value={granularity}
                    onChange={setChosenGranularity}
                    ariaLabel={t("granularityLabel")}
                  />
                ) : null}
                <ChartViewToggle
                  options={rangeOptions}
                  value={metric === "rate" ? rateRange(range) : range}
                  onChange={onRangeChange}
                  ariaLabel={t("rangeLabel")}
                />
              </div>
            </div>
            {listing.empty ? (
              <EmptyWindow metric={metric} range={metric === "rate" ? rateRange(range) : range} />
            ) : metric === "rate" ? (
              <RateChart
                trends={trends}
                series={series}
                selected={listing.drawn}
                range={range}
                now={now}
                view={rateView}
              />
            ) : metric === "sales" ? (
              <SalesChart
                trends={trends}
                series={series}
                selected={listing.drawn}
                range={range}
                granularity={granularity}
                now={now}
              />
            ) : (
              <ClicksChart
                trends={trends}
                series={series}
                selected={listing.drawn}
                range={range}
                granularity={granularity}
                now={now}
              />
            )}
            {/* Beneath the scrolling window, where a sentence can be read whole.
                It restates ADR 0022's caveat because these are the numbers most
                tempting to read as people: they are loads, and a floor. */}
            <p className="text-xs text-muted-foreground">{t("floorNote")}</p>
            {metric === "rate" ? (
              <p className="text-xs text-muted-foreground">{t("rateNote")}</p>
            ) : null}
          </>
        ) : (
          <EmptyState />
        )}
      </CardContent>
    </Card>
  );
}

/**
 * The chart itself, kept apart from the state above it, drawn at a width worked
 * out from the span and read by scrolling when the span outgrows the card —
 * the Sales Trends Daily rule, because an hour here is read one bucket at a
 * time just as a day is there. A line per series, not a group of bars: with a
 * dozen links drawn a bar per link per hour is a sliver nobody can read, while
 * a line reads at any series count (ADR 0057). A zero-click hour is a zero,
 * not a gap, so the series stay zero-filled and the lines unbroken.
 */
function ClicksChart({
  trends,
  series,
  selected,
  range,
  granularity,
  now,
}: {
  trends: AffiliateTrends;
  series: StackedSeries[];
  selected: readonly string[];
  range: AffiliateTrendsRange;
  granularity: AffiliateTrendsGranularity;
  now: Date;
}) {
  const t = useTranslations("affiliateTrends");
  const locale = toAppLocale(useLocale());
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotShare = availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0;

  // Recomputed on every chip click, range switch and granularity switch, and
  // on nothing else: the buckets are already in hand.
  const data = useMemo(
    () => viewsSeries(trends, selected, range, now, locale, granularity),
    [trends, selected, range, now, locale, granularity],
  );
  const yMax = useMemo(() => multiSeriesYMax(data), [data]);
  const yTicks = useMemo(() => countYTicks(yMax), [yMax]);
  const plotWidth = affiliateTrendsPlotWidth(data.length, plotShare);
  useLatestBucketsFirst(scrollArea, `${range}:${granularity}`, plotWidth, data);

  const hourly = granularity === "hour";
  return (
    <ChartScrollArea ref={setScrollArea} ariaLabel={t("scrollLabel")}>
      <MultiSeriesLineChart
        data={data}
        series={series}
        yMax={yMax}
        yTicks={yTicks}
        plotWidth={plotWidth}
        formatValue={(value) => formatNumber(value, locale)}
        nothingCountedLabel={t("tooltipNothingCounted")}
        ariaLabel={hourly ? t("chartLabelHourly") : t("chartLabelDaily")}
      />
    </ChartScrollArea>
  );
}

/**
 * The Attributed Sales view: each link's attributed active sales over the same
 * axis, ranges and legend as the Clicks view, with the bucket's tickets and Net
 * Proceeds stated in the tooltip — so links are weighed by money as well as by
 * count. A Reversal is already out of the payload, so it is out of every point.
 */
function SalesChart({
  trends,
  series,
  selected,
  range,
  granularity,
  now,
}: {
  trends: AffiliateTrends;
  series: StackedSeries[];
  selected: readonly string[];
  range: AffiliateTrendsRange;
  granularity: AffiliateTrendsGranularity;
  now: Date;
}) {
  const t = useTranslations("affiliateTrends");
  const locale = toAppLocale(useLocale());
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotShare = availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0;

  const data = useMemo(
    () => salesSeries(trends, selected, range, now, locale, granularity),
    [trends, selected, range, now, locale, granularity],
  );
  const yMax = useMemo(() => multiSeriesYMax(data), [data]);
  const yTicks = useMemo(() => countYTicks(yMax), [yMax]);
  const plotWidth = affiliateTrendsPlotWidth(data.length, plotShare);
  useLatestBucketsFirst(scrollArea, `${range}:${granularity}`, plotWidth, data);

  // The money line under a link's tooltip row. The sale count is the point; the
  // tickets and Net Proceeds are what the count was worth, in the
  // Organization's currency however the reader's language spells it.
  const formatSeriesDetail = useCallback(
    (seriesId: string, datum: { key: string; label: string }) => {
      const figures = (datum as AffiliateSalesDatum).details?.[seriesId];
      if (!figures) {
        return null;
      }
      return t("salesDetail", {
        tickets: figures.tickets,
        amount: formatMoney(figures.netProceedsCents, trends.currency, locale),
      });
    },
    [t, trends.currency, locale],
  );

  const hourly = granularity === "hour";
  return (
    <ChartScrollArea ref={setScrollArea} ariaLabel={t("scrollLabel")}>
      <MultiSeriesLineChart
        data={data}
        series={series}
        yMax={yMax}
        yTicks={yTicks}
        plotWidth={plotWidth}
        formatValue={(value) => formatNumber(value, locale)}
        formatSeriesDetail={formatSeriesDetail}
        nothingCountedLabel={t("tooltipNothingCounted")}
        ariaLabel={hourly ? t("salesChartLabelHourly") : t("salesChartLabelDaily")}
      />
    </ChartScrollArea>
  );
}

/**
 * The Attribution Rate view: per link, Attributed Sales over Clicks, and one
 * overall line dividing every attributed sale by every page view. Lines like
 * the counting views, but never hourly — daily points at most, Cumulative by default
 * (ADR 0057). A span with no clicks draws a gap, not a zero; the tooltip
 * states the division behind each point, because a bare percentage built on
 * two floors invites more belief than it earned.
 */
function RateChart({
  trends,
  series,
  selected,
  range,
  now,
  view,
}: {
  trends: AffiliateTrends;
  series: StackedSeries[];
  selected: readonly string[];
  range: AffiliateTrendsRange;
  now: Date;
  view: AffiliateRateView;
}) {
  const t = useTranslations("affiliateTrends");
  const locale = toAppLocale(useLocale());
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotShare = availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0;

  const data = useMemo(
    () => rateSeries(trends, selected, range, now, locale, view),
    [trends, selected, range, now, locale, view],
  );
  const yMax = useMemo(() => rateYMax(data), [data]);
  const yTicks = useMemo(() => rateYTicks(yMax), [yMax]);
  // A rate is read as a shape, not a point at a time, so it fits the card the
  // way Sales Trends' Cumulative view does rather than scrolling.
  const plotWidth = cumulativePlotWidth(plotShare);

  // The division under a point: what was divided by what. The overall line
  // divides by page views and says so; a link's line divides by its clicks.
  const formatSeriesDetail = useCallback(
    (seriesId: string, datum: { key: string; label: string }) => {
      const figures = (datum as AffiliateRateDatum).details?.[seriesId];
      if (!figures) {
        return null;
      }
      return seriesId === ALL_PAGE_VIEWS_ID
        ? t("rateDetailOverall", { sales: figures.sales, views: figures.denominator })
        : t("rateDetail", { sales: figures.sales, clicks: figures.denominator });
    },
    [t],
  );

  return (
    <ChartScrollArea ref={setScrollArea} ariaLabel={t("rateScrollLabel")}>
      <MultiSeriesLineChart
        data={data}
        series={series}
        yMax={yMax}
        yTicks={yTicks}
        plotWidth={plotWidth}
        formatValue={(value) => formatPercent(value, locale)}
        formatSeriesDetail={formatSeriesDetail}
        nothingCountedLabel={t("tooltipNothingCounted")}
        zeroIsMeasured
        ariaLabel={view === "cumulative" ? t("rateChartLabelCumulative") : t("rateChartLabelDaily")}
      />
    </ChartScrollArea>
  );
}

/**
 * The element's current inner width, tracked as it changes — the same
 * measurement Sales Trends takes, for the same reason: the charts are drawn at
 * a width in pixels, and the card's width answers to the viewport.
 */
function useElementWidth(element: HTMLElement | null): number {
  const [width, setWidth] = useState(0);
  useEffect(() => {
    if (!element) {
      return;
    }
    const measure = () => setWidth(element.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [element]);
  return width;
}

/**
 * Opens the scrolling window on the newest bucket that has anything in it:
 * the reader came to see what happened last, and on a live hourly range "now"
 * is routinely a run of still-empty hours — anchored there, the viewport shows
 * a row of zeros and nothing says the data is off to the left. A window with
 * nothing in it anchors at the plot's end, as before. Re-anchors when the range
 * or granularity changes and otherwise leaves the reader's scrolling alone —
 * the Sales Trends anchor, keyed on the axis instead of the view.
 */
function useLatestBucketsFirst(
  element: HTMLElement | null,
  anchorKey: string,
  plotWidth: number,
  data: readonly { values: Record<string, number | null> }[],
): void {
  const anchor = useRef({ key: "", taken: false });
  useLayoutEffect(() => {
    if (anchor.current.key !== anchorKey) {
      anchor.current = { key: anchorKey, taken: false };
    }
    if (!element || anchor.current.taken) {
      return;
    }
    const overflow = element.scrollWidth - element.clientWidth;
    if (overflow <= 0) {
      return;
    }
    element.scrollLeft = latestBucketsScrollLeft(data, {
      plotWidth,
      plotLeft: Y_AXIS_WIDTH,
      plotRight: CHART_MARGIN.right,
      clientWidth: element.clientWidth,
      scrollWidth: element.scrollWidth,
    });
    anchor.current.taken = true;
  }, [element, anchorKey, plotWidth, data]);
}

/**
 * What a window with nothing in it is told, in the chart's place: the span by
 * name, and on the sales and rate views the metric too, so the reader knows
 * which reading is empty and that widening the range is the way out — the
 * controls stay above it for exactly that. Distinct from `EmptyState`, which
 * is the whole history being empty (#431).
 */
function EmptyWindow({
  metric,
  range,
}: {
  metric: AffiliateTrendsMetric;
  range: AffiliateTrendsRange;
}) {
  const t = useTranslations("affiliateTrends");
  return (
    <div className="rounded-md border border-dashed px-6 py-12 text-center">
      <p className="text-sm text-muted-foreground">
        {t(`emptyWindow_${metric}`, { window: t(`window_${range}`) })}
      </p>
    </div>
  );
}

/**
 * What an Event with nothing counted yet is told. Counting began at this
 * feature's launch (ADR 0057), so an older Event's earlier clicks live in the
 * lifetime figures on the Affiliate Links tab (#464), not here — the empty
 * state says so rather than
 * letting the gap read as lost data.
 */
function EmptyState() {
  const t = useTranslations("affiliateTrends");
  return (
    <div className="rounded-md border border-dashed px-6 py-12 text-center">
      <p className="text-sm font-medium">{t("emptyTitle")}</p>
      <p className="mt-1 text-sm text-muted-foreground">{t("emptyBody")}</p>
    </div>
  );
}
