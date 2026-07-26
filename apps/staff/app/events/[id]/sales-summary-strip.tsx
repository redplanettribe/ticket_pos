"use client";

import { useEffect, useState } from "react";

import { Card, CardContent, Skeleton } from "@ticket-pos/ui";

import { formatPriceCents } from "@/lib/events-api";
import { fetchSalesSummary, type EventSalesSummary } from "@/lib/sales-api";

import { useSalesRefreshSignal } from "./sales-refresh";

type SalesSummaryStripProps = {
  eventId: string;
};

// The Sales tab's stat strip: one honest take-home figure for the Event, above
// the list of the sales that produced it. Net Proceeds is what the Event's
// Online Sales have left the Organization once platform costs are out; the
// platform's cut is never shown as a number (ADR 0014). Rendered only for Org
// Admins and Event Owners — Event Staff get the Sales list on its own.
export function SalesSummaryStrip({ eventId }: SalesSummaryStripProps) {
  const [summary, setSummary] = useState<EventSalesSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // Bumped by the import section on a successful commit/undo: those change the
  // Event's sales count, so the strip re-reads alongside the list.
  const refreshSignal = useSalesRefreshSignal();

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchSalesSummary(eventId)
      .then((data) => {
        if (!cancelled) {
          setSummary(data);
        }
      })
      .catch((fetchError: unknown) => {
        if (!cancelled) {
          setError(fetchError instanceof Error ? fetchError.message : "Failed to load the summary");
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
  }, [eventId, refreshSignal]);

  return (
    <Card>
      <CardContent className="grid gap-6 py-6 sm:grid-cols-2">
        <Stat
          label="Net proceeds"
          hint="What your online sales have earned this Event, after platform costs."
          loading={loading}
          error={error}
          value={summary ? formatPriceCents(summary.net_proceeds_cents, summary.currency) : null}
        />
        <Stat
          label="Sales"
          hint="Active Ticket Sales recorded for this Event."
          loading={loading}
          error={error}
          value={summary ? String(summary.sales_count) : null}
        />
      </CardContent>
    </Card>
  );
}

type StatProps = {
  label: string;
  hint: string;
  loading: boolean;
  error: string | null;
  value: string | null;
};

function Stat({ label, hint, loading, error, value }: StatProps) {
  return (
    <div className="space-y-1">
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      {loading ? (
        <Skeleton className="h-8 w-32" />
      ) : error || value === null ? (
        <p className="text-sm text-muted-foreground">—</p>
      ) : (
        <p className="text-2xl font-semibold tabular-nums">{value}</p>
      )}
      <p className="text-xs text-muted-foreground">{error ? `Couldn't load: ${error}` : hint}</p>
    </div>
  );
}
