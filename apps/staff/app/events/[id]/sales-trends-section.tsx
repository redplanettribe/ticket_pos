"use client";

import { useCallback, useEffect, useMemo, useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  ChartLegendChips,
  ChartScrollArea,
  Skeleton,
  chartSeriesColor,
  type ChartLegendChip,
  type StackedBarSeries,
} from "@ticket-pos/ui";

import { reversedCountLabel } from "@/lib/sales-api";
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

/** Tickets are whole things; the axis and the tooltip both count them plainly. */
const formatTickets = (value: number) => new Intl.NumberFormat().format(value);

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
 */
export function SalesTrendsSection({ eventId }: SalesTrendsSectionProps) {
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
          setLoadError(error instanceof Error ? error.message : "Failed to load sales trends");
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
        <CardTitle>Trends</CardTitle>
        <CardDescription>
          How this Event has sold, day by day, in the Event&apos;s own timezone. Reversed sales are
          left out, as they are everywhere else.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <Skeleton className="h-80 w-full" />
        ) : loadError ? (
          <Alert variant="destructive">
            <AlertTitle>Could not load sales trends</AlertTitle>
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
                  ariaLabel="Ticket Types drawn on the chart"
                />
                <TrendsCharts trends={trends} series={series} />
              </>
            ) : (
              <EmptyState />
            )}
            {/* Stated whether or not there is a chart: an Event whose every sale
                was reversed has nothing to plot, and "no sales yet" on its own
                would be a lie the reader could not check. */}
            <p className="text-xs text-muted-foreground">
              {reversedCountLabel(trends.reversed_count)}
              {trends.reversed_count > 0
                ? " — left out of every figure here, which is why a day can be smaller than you remember."
                : "."}
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
function TrendsCharts({ trends, series }: { trends: SalesTrends; series: StackedBarSeries[] }) {
  const plotWidth = trendsPlotWidth(trends.days.length);
  return (
    <div className="space-y-2">
      <ChartScrollArea ariaLabel="Sales Trends charts — scroll sideways to move through the Event's selling period">
        <div className="w-max space-y-6">
          <TrendsChart
            title="Tickets sold"
            description="How much stock moved each day, stacked by Ticket Type."
            days={trends.days}
            series={series}
            plotWidth={plotWidth}
            measure="quantity"
            formatValue={formatTickets}
            totalLabel="Total tickets"
            syncId={TRENDS_SYNC_ID}
          />
          <TrendsChart
            title="Takings"
            description="What this Event made each day, stacked by Ticket Type."
            days={trends.days}
            series={series}
            plotWidth={plotWidth}
            measure="takings_cents"
            formatValue={(value) => formatTakings(value, trends.currency)}
            formatTickValue={(value) => formatTakingsTick(value, trends.currency)}
            totalLabel="Total takings"
            syncId={TRENDS_SYNC_ID}
          />
        </div>
      </ChartScrollArea>
      {/* Beneath the scrolling window rather than inside it. A paragraph laid out
          across a plot several thousand pixels wide would be one very long line
          the reader had to scroll to finish, and this one is here to be read. */}
      <p className="text-xs text-muted-foreground">{TAKINGS_NOTE}</p>
    </div>
  );
}

/** Ties the two charts' hover together. One value, used twice, on purpose. */
const TRENDS_SYNC_ID = "sales-trends";

/**
 * Why this chart's total is bigger than the one on the Sales tab.
 *
 * Takings counts every Sales Channel and Net proceeds counts online alone
 * (ADR 0040), so on any Event that sold at the door or imported its history the
 * two figures differ — correctly, and by a lot. Somebody meeting that unlabelled
 * files a bug. This says which figure is which and leaves it at that: the note
 * is here to make the gap legible, not to teach anyone the platform's costs.
 */
const TAKINGS_NOTE =
  "Takings is what the Event made wherever it sold — online, at the door, and in any sales you imported. " +
  "Net proceeds on the Sales tab counts your online sales alone, so the two figures are answering different " +
  "questions and a bigger number here is the rest of your selling showing up. Free Ticket Types move stock " +
  "without earning anything, so they stack in the chart above and add nothing to this one.";

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
  return (
    <div className="rounded-md border border-dashed px-6 py-12 text-center">
      <p className="text-sm font-medium">No sales to chart yet</p>
      <p className="mt-1 text-sm text-muted-foreground">
        This Event has not sold a ticket, so there is nothing to plot. Trends fills in as sales
        arrive — and an Event that sends its audience elsewhere to register never sells one here, so
        it stays empty on purpose.
      </p>
    </div>
  );
}
