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
  CHART_PLOT_INSET,
  ChartLegendChips,
  ChartScrollArea,
  ChartViewToggle,
  MultiSeriesBarChart,
  Skeleton,
  chartSeriesColor,
  type ChartLegendChip,
  type ChartViewOption,
  type StackedSeries,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import {
  AFFILIATE_TRENDS_RANGES,
  ALL_PAGE_VIEWS_ID,
  DEFAULT_AFFILIATE_TRENDS_RANGE,
  affiliateTrendsPlotWidth,
  availableTrendsMetrics,
  fetchAffiliateTrends,
  hasTrendsData,
  multiSeriesYMax,
  rangeGranularity,
  salesSeries,
  toggleSeriesSelection,
  viewsSeries,
  type AffiliateSalesDatum,
  type AffiliateTrends,
  type AffiliateTrendsMetric,
  type AffiliateTrendsRange,
} from "@/lib/affiliate-trends";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatMoney, formatNumber } from "@/lib/format";
import { trendsYTicks } from "@/lib/sales-trends";

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
  // Which measure the one chart draws. Clicks first — traffic is the tab's
  // first question, and the only one an externally registered Event can answer.
  const [metric, setMetric] = useState<AffiliateTrendsMetric>("clicks");

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
  // chips are off and whenever the tab is opened.
  const order = useMemo(
    () => [ALL_PAGE_VIEWS_ID, ...(trends?.links.map((link) => link.id) ?? [])],
    [trends],
  );
  const nameOf = useCallback(
    (id: string) =>
      id === ALL_PAGE_VIEWS_ID
        ? t("allPageViews")
        : (trends?.links.find((link) => link.id === id)?.name ?? id),
    [trends, t],
  );
  const colorOf = useCallback((id: string) => chartSeriesColor(order.indexOf(id)), [order]);

  // The sales view draws links only: the whole-page series is a views concept,
  // so its chip leaves the legend rather than sitting beside bars it can never
  // have. The selection itself is shared — chips chosen on one view hold on the
  // other, and the whole-page chip's state simply waits for the Clicks view.
  const chartOrder = useMemo(
    () => (metric === "clicks" ? order : order.filter((id) => id !== ALL_PAGE_VIEWS_ID)),
    [order, metric],
  );

  const chips: ChartLegendChip[] = useMemo(
    () => chartOrder.map((id) => ({ id, label: nameOf(id), color: colorOf(id) })),
    [chartOrder, nameOf, colorOf],
  );

  const rangeOptions: ChartViewOption<AffiliateTrendsRange>[] = useMemo(
    () => AFFILIATE_TRENDS_RANGES.map((id) => ({ id, label: t(`range_${id}`) })),
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
    () =>
      chartOrder
        .filter((id) => selected.includes(id))
        .map((id) => ({ id, name: nameOf(id), color: colorOf(id) })),
    [chartOrder, selected, nameOf, colorOf],
  );

  const onToggle = useCallback(
    (id: string) => {
      setSelected((current) => {
        // The last chip THIS view draws cannot be deselected — on the sales
        // view a still-selected whole-page series is not drawn, so it must not
        // count as "something is still on the chart".
        const drawnHere = current.filter((entry) => chartOrder.includes(entry));
        if (drawnHere.length <= 1 && drawnHere.includes(id)) {
          return current;
        }
        return toggleSeriesSelection(order, current, id);
      });
    },
    [order, chartOrder],
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
              <ChartLegendChips
                chips={chips}
                selected={selected}
                onToggle={onToggle}
                ariaLabel={t("chipsLabel")}
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
                <ChartViewToggle
                  options={rangeOptions}
                  value={range}
                  onChange={setRange}
                  ariaLabel={t("rangeLabel")}
                />
              </div>
            </div>
            {metric === "sales" ? (
              <SalesChart
                trends={trends}
                series={series}
                selected={selected}
                range={range}
                now={now}
              />
            ) : (
              <ClicksChart
                trends={trends}
                series={series}
                selected={selected}
                range={range}
                now={now}
              />
            )}
            {/* Beneath the scrolling window, where a sentence can be read whole.
                It restates ADR 0022's caveat because these are the numbers most
                tempting to read as people: they are loads, and a floor. */}
            <p className="text-xs text-muted-foreground">{t("floorNote")}</p>
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
 * the Sales Trends Daily rule, because an hour here is read one bar at a time
 * just as a day is there.
 */
function ClicksChart({
  trends,
  series,
  selected,
  range,
  now,
}: {
  trends: AffiliateTrends;
  series: StackedSeries[];
  selected: readonly string[];
  range: AffiliateTrendsRange;
  now: Date;
}) {
  const t = useTranslations("affiliateTrends");
  const locale = toAppLocale(useLocale());
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotShare = availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0;

  // Recomputed on every chip click and range switch, and on nothing else: the
  // buckets are already in hand.
  const data = useMemo(
    () => viewsSeries(trends, selected, range, now, locale),
    [trends, selected, range, now, locale],
  );
  const yMax = useMemo(() => multiSeriesYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  const plotWidth = affiliateTrendsPlotWidth(data.length, series.length, plotShare);
  useLatestBucketsFirst(scrollArea, range, plotWidth);

  const hourly = rangeGranularity(range) === "hour";
  return (
    <ChartScrollArea ref={setScrollArea} ariaLabel={t("scrollLabel")}>
      <MultiSeriesBarChart
        data={data}
        series={series}
        yMax={yMax}
        yTicks={yTicks}
        plotWidth={plotWidth}
        formatValue={(value) => formatNumber(value, locale)}
        ariaLabel={hourly ? t("chartLabelHourly") : t("chartLabelDaily")}
      />
    </ChartScrollArea>
  );
}

/**
 * The Attributed Sales view: each link's attributed active sales over the same
 * axis, ranges and legend as the Clicks view, with the bucket's tickets and Net
 * Proceeds stated in the tooltip — so links are weighed by money as well as by
 * count. A Reversal is already out of the payload, so it is out of every bar.
 */
function SalesChart({
  trends,
  series,
  selected,
  range,
  now,
}: {
  trends: AffiliateTrends;
  series: StackedSeries[];
  selected: readonly string[];
  range: AffiliateTrendsRange;
  now: Date;
}) {
  const t = useTranslations("affiliateTrends");
  const locale = toAppLocale(useLocale());
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  const plotShare = availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0;

  const data = useMemo(
    () => salesSeries(trends, selected, range, now, locale),
    [trends, selected, range, now, locale],
  );
  const yMax = useMemo(() => multiSeriesYMax(data), [data]);
  const yTicks = useMemo(() => trendsYTicks(yMax), [yMax]);
  const plotWidth = affiliateTrendsPlotWidth(data.length, series.length, plotShare);
  useLatestBucketsFirst(scrollArea, range, plotWidth);

  // The money line under a link's tooltip row. The sale count is the bar; the
  // tickets and Net Proceeds are what the count was worth, in the
  // Organization's currency however the reader's language spells it.
  const formatSeriesDetail = useCallback(
    (seriesId: string, datum: { key: string; label: string; values: Record<string, number> }) => {
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

  const hourly = rangeGranularity(range) === "hour";
  return (
    <ChartScrollArea ref={setScrollArea} ariaLabel={t("scrollLabel")}>
      <MultiSeriesBarChart
        data={data}
        series={series}
        yMax={yMax}
        yTicks={yTicks}
        plotWidth={plotWidth}
        formatValue={(value) => formatNumber(value, locale)}
        formatSeriesDetail={formatSeriesDetail}
        ariaLabel={hourly ? t("salesChartLabelHourly") : t("salesChartLabelDaily")}
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
 * Opens the scrolling window on the most recent buckets: left is a week ago,
 * right is this hour, and the reader came to see now. Re-anchors when the range
 * changes and otherwise leaves the reader's scrolling alone — the Sales Trends
 * anchor, keyed on the range instead of the view.
 */
function useLatestBucketsFirst(
  element: HTMLElement | null,
  anchorKey: string,
  plotWidth: number,
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
    element.scrollLeft = overflow;
    anchor.current.taken = true;
  }, [element, anchorKey, plotWidth]);
}

/**
 * What an Event with nothing counted yet is told. Counting began at this
 * feature's launch (ADR 0057), so an older Event's earlier clicks live in the
 * lifetime figures above, not here — the empty state says so rather than
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
