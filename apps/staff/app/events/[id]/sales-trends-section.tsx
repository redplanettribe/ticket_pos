"use client";

import { useCallback, useEffect, useMemo, useState } from "react";

import { toAppLocale, type AppLocale } from "@ticket-pos/locale";
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
  Skeleton,
  chartSeriesColor,
  type ChartLegendChip,
  type StackedBarSeries,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatNumber } from "@/lib/format";
import {
  colorSlotFor,
  drawnTicketTypes,
  fetchSalesTrends,
  formatTakings,
  formatTakingsTick,
  hasSales,
  toggleTicketTypeSelection,
  trendsPlotWidth,
  type SalesTrends,
} from "@/lib/sales-trends";

import { TrendsChart } from "./trends-chart";

type SalesTrendsSectionProps = {
  eventId: string;
};

/**
 * The Sales Trends surface: how this Event's sales moved, day by day, split by
 * Ticket Type.
 *
 * Everything arrives in one request, and this component is what holds it: the
 * day × Ticket Type matrix, and the chip selection over it. Both live here
 * rather than inside a chart because the surface is a pair of charts sharing one
 * selection (#277) — a selection held inside a chart could only ever filter that
 * chart, and keeping two of them in sync is exactly the bug the shared state
 * avoids.
 *
 * Every figure it draws goes through lib/format.ts in the reader's Staff Locale,
 * and every figure it draws is stated in facts the reader's language has no vote
 * in: Takings in the currency the API named, and each bar on the calendar day the
 * API already resolved into the EVENT's timezone. A Spanish reader sees the same
 * day and the same money as an English one, with Spanish marks on both.
 */
export function SalesTrendsSection({ eventId }: SalesTrendsSectionProps) {
  const t = useTranslations("trends");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [trends, setTrends] = useState<SalesTrends | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  // Which Ticket Types are drawn. Every type starts selected: the first look is
  // the whole Event, and narrowing is the reader's move to make.
  const [selected, setSelected] = useState<string[]>([]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    fetchSalesTrends(eventId)
      .then((data) => {
        if (cancelled) {
          return;
        }
        setTrends(data);
        setSelected(data.ticket_types.map((type) => type.id));
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

  const catalog = useMemo(() => trends?.ticket_types ?? [], [trends]);

  // Colour is keyed on position in the whole catalog, never on position in the
  // drawn set: a Ticket Type keeps its colour whichever chips are off and
  // whenever the tab is opened, which is what lets staff learn the chart.
  //
  // Position rather than sort_order itself, because sort_order defaults to 0 and
  // an Organization that never reordered its catalog has every Ticket Type at 0
  // — which would draw the whole stack in one colour and make the chart
  // unreadable for exactly the Events nobody has fussed over. The catalog
  // arrives in display order, so position carries that order without the ties.
  const colorOf = useCallback(
    (id: string) => chartSeriesColor(colorSlotFor(catalog, id)),
    [catalog],
  );

  // A Ticket Type's name is the Organization's own word: it is data, and reads
  // as coined in both languages.
  const chips: ChartLegendChip[] = useMemo(
    () => catalog.map((type) => ({ id: type.id, label: type.name, color: colorOf(type.id) })),
    [catalog, colorOf],
  );

  // The drawn series, in catalog display order — the order the bars stack in.
  const series: StackedBarSeries[] = useMemo(
    () =>
      drawnTicketTypes(catalog, selected).map((type) => ({
        id: type.id,
        name: type.name,
        color: colorOf(type.id),
      })),
    [catalog, selected, colorOf],
  );

  // Purely local: no fetch, no URL write, no server round trip.
  const onToggle = useCallback(
    (id: string) => {
      setSelected((current) => toggleTicketTypeSelection(catalog, current, id));
    },
    [catalog],
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
        ) : !trends ? null : (
          <>
            {hasSales(trends) ? (
              <>
                <ChartLegendChips
                  chips={chips}
                  selected={selected}
                  onToggle={onToggle}
                  ariaLabel={t("chipsLabel")}
                />
                <TrendsCharts trends={trends} series={series} locale={locale} />
              </>
            ) : (
              <EmptyState />
            )}
            {/* Stated whether or not there is a chart: an Event whose every sale
                was reversed has nothing to plot, and "no sales yet" on its own
                would be a lie the reader could not check.

                Two whole messages rather than a count with a clause glued after
                it: the sentence about a quiet Event is not the reversed sentence
                minus its tail, and in Spanish it is not even the same shape. */}
            <p className="text-xs text-muted-foreground">
              {trends.reversed_count > 0
                ? t("reversedNotice", { count: trends.reversed_count })
                : t("reversedNone")}
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}

/**
 * Every chart on the surface, kept apart from the state above them.
 *
 * The pair is the feature. Both charts are handed the same days and the same
 * drawn series, so one chip click moves both and they can never disagree about
 * which Ticket Types are on. They share a `syncId`, so pointing at a day in
 * either highlights that day in the other — which is how a Saturday that moved
 * the most tickets and earned less than the Thursday is read at all.
 *
 * They stack vertically on every width rather than sitting side by side: the
 * comparison is between two figures for the same day, so the days must share a
 * horizontal position, and a narrow screen therefore degrades into reading one
 * chart and then the other rather than into a broken layout.
 *
 * That horizontal position is why both are drawn at one width, worked out here
 * from the span, and why both are drawn inside one scrolling window. A window
 * each would need their offsets kept equal by hand, and the day under the
 * pointer in the upper chart would sooner or later stop being the day under it
 * in the lower one. One window has one offset and cannot disagree with itself.
 */
function TrendsCharts({
  trends,
  series,
  locale,
}: {
  trends: SalesTrends;
  series: StackedBarSeries[];
  locale: AppLocale;
}) {
  const t = useTranslations("trends");
  const [scrollArea, setScrollArea] = useState<HTMLDivElement | null>(null);
  const availableWidth = useElementWidth(scrollArea);
  // The plot fills the card when the span fits inside it, and overflows it when
  // the span does not. Measured rather than assumed: the card's width answers to
  // the viewport and to the sidebar, so it is not something this component can
  // work out from what it was passed.
  const plotWidth = trendsPlotWidth(
    trends.days.length,
    availableWidth > 0 ? availableWidth - CHART_PLOT_INSET : 0,
  );
  const ticketsTitle = t("ticketsTitle");
  const takingsTitle = t("takingsTitle");
  return (
    <div className="space-y-2">
      <ChartScrollArea ref={setScrollArea} ariaLabel={t("scrollLabel")}>
        <div className="w-max space-y-6">
          <TrendsChart
            title={ticketsTitle}
            description={t("ticketsDescription")}
            days={trends.days}
            series={series}
            plotWidth={plotWidth}
            measure="quantity"
            locale={locale}
            // Tickets are whole things; the axis and the tooltip both count them
            // plainly, with the reader's own thousands mark and nobody else's.
            formatValue={(value) => formatNumber(value, locale)}
            totalLabel={t("ticketsTotal")}
            ariaLabel={t("chartLabel", { title: ticketsTitle })}
            syncId={TRENDS_SYNC_ID}
          />
          <TrendsChart
            title={takingsTitle}
            description={t("takingsDescription")}
            days={trends.days}
            series={series}
            plotWidth={plotWidth}
            measure="takings_cents"
            locale={locale}
            formatValue={(value) => formatTakings(value, trends.currency, locale)}
            formatTickValue={(value) => formatTakingsTick(value, trends.currency, locale)}
            totalLabel={t("takingsTotal")}
            ariaLabel={t("chartLabel", { title: takingsTitle })}
            syncId={TRENDS_SYNC_ID}
          />
        </div>
      </ChartScrollArea>
      {/* Beneath the scrolling window rather than inside it. A paragraph laid out
          across a plot several thousand pixels wide would be one very long line
          the reader had to scroll to finish, and this one is here to be read.

          Why it exists: Takings counts every Sales Channel and Net proceeds
          counts online alone (ADR 0040), so on any Event that sold at the door or
          imported its history the two figures differ — correctly, and by a lot.
          Somebody meeting that unlabelled files a bug. */}
      <p className="text-xs text-muted-foreground">{t("takingsNote")}</p>
    </div>
  );
}

/** Ties the two charts' hover together. One value, used twice, on purpose. */
const TRENDS_SYNC_ID = "sales-trends";

/**
 * The element's current inner width, tracked as it changes.
 *
 * The charts are drawn at a width in pixels rather than stretched by CSS, so
 * something has to say how many pixels are going spare — and that answer moves
 * when the window resizes or the sidebar collapses. `clientWidth` rather than
 * the observer's `contentRect`, because it is the scrollable box's own inner
 * width and so already excludes any scrollbar the charts themselves provoked.
 *
 * Zero until the element is measured, which is the server-rendered pass and the
 * first client frame; the caller reads that as "the span decides alone".
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
 * What an Event with nothing to chart is told.
 *
 * It says the Event has sold nothing rather than drawing an empty grid, which
 * would read as broken. An Event with External Registration lands here too: it
 * sells no Ticket Sale at all, so it has nothing to chart by definition rather
 * than by bad luck — hence the second sentence, which names that case instead of
 * leaving the reader to wonder whether the tab is failing.
 */
function EmptyState() {
  const t = useTranslations("trends");
  return (
    <div className="rounded-md border border-dashed px-6 py-12 text-center">
      <p className="text-sm font-medium">{t("emptyTitle")}</p>
      <p className="mt-1 text-sm text-muted-foreground">{t("emptyBody")}</p>
    </div>
  );
}
